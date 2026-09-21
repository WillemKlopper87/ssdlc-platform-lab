#Requires -Version 5.1
<#
.SYNOPSIS
  One-shot UAT deployment of the SSDLC platform on a Windows server with
  Docker Desktop. Re-runnable: existing secrets and accounts are reused.

.DESCRIPTION
  Installs Terraform if missing (and puts it on PATH), opens the LAN firewall
  ports, generates secrets, provisions the Docker network/volumes, starts
  Gitea + Woodpecker + Postgres + the DAST target + the portal + the bot
  approver, and creates the accounts:
    gateadmin  (admin)          dev1, dev2, dev3 (developers)
  plus two service accounts (gate-bot, gate-reporter).
  Ollama / Continue is deliberately NOT part of this deployment.

  Run from an elevated PowerShell:
    .\deploy\uat\setup-uat.ps1 -ServerIp 192.168.1.28
#>
[CmdletBinding()]
param(
    [string]$ServerIp = '192.168.1.28',
    [string]$TerraformVersion = '1.15.0',
    [switch]$SkipFirewall
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot   = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$StateDir   = Join-Path $PSScriptRoot 'state'
$EnvFile    = Join-Path $StateDir 'uat.env'
$CredFile   = Join-Path $StateDir 'credentials.txt'
$BaseCompose = Join-Path $RepoRoot 'compose\minimal\docker-compose.yml'
$UatCompose  = Join-Path $PSScriptRoot 'docker-compose.uat.yml'
$Dockerfile  = Join-Path $PSScriptRoot 'Dockerfile'
$TfDir       = Join-Path $RepoRoot 'terraform\local'
$GiteaUrl    = "http://${ServerIp}:3500"
$WpUrl       = "http://${ServerIp}:8000"
$PortalUrl   = "http://${ServerIp}:8181"

function Step($m)  { Write-Host "`n==> $m" -ForegroundColor Cyan }
function Info($m)  { Write-Host "    $m" }
function Fail($m)  { throw $m }

# Windows PowerShell 5.1 turns redirected native stderr into terminating
# errors under $ErrorActionPreference='Stop'; probes go through this instead.
function Try-Native([scriptblock]$Block) {
    $old = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    try { & $Block } finally { $ErrorActionPreference = $old }
}
function Assert-Native([string]$What) {
    if ($LASTEXITCODE -ne 0) { Fail "$What failed (exit code $LASTEXITCODE)" }
}

# --------------------------------------------------------------- env file
$Script:Cfg = [ordered]@{}
function Load-Env {
    $Script:Cfg = [ordered]@{}
    if (Test-Path $EnvFile) {
        foreach ($line in Get-Content $EnvFile) {
            if ($line -match '^\s*([A-Za-z0-9_]+)=(.*)$') { $Script:Cfg[$Matches[1]] = $Matches[2] }
        }
    }
}
function Save-Env {
    New-Item -ItemType Directory -Force -Path $StateDir | Out-Null
    $lines = $Script:Cfg.GetEnumerator() | ForEach-Object { "$($_.Key)=$($_.Value)" }
    [IO.File]::WriteAllText($EnvFile, (($lines -join "`n") + "`n"), (New-Object Text.UTF8Encoding $false))
}
function Set-Cfg([string]$k, [string]$v) { $Script:Cfg[$k] = $v; Save-Env }
function Merge-Output($lines) {
    foreach ($l in $lines) { if ($l -match '^([A-Z0-9_]+)=(.+)$') { $Script:Cfg[$Matches[1]] = $Matches[2] } }
    Save-Env
}
function New-Secret([int]$len = 24) {
    # GetInt32 is .NET Core only; Windows PowerShell 5.1 needs GetBytes.
    # Rejection sampling keeps the choice unbiased (chars.Length = 57).
    $chars = [char[]]('abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789')
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    $limit = 256 - (256 % $chars.Length)
    $buf = New-Object byte[] 1
    do {
        $sb = New-Object Text.StringBuilder
        while ($sb.Length -lt $len) {
            $rng.GetBytes($buf)
            if ($buf[0] -lt $limit) { [void]$sb.Append($chars[$buf[0] % $chars.Length]) }
        }
        $s = $sb.ToString()
    } until ($s -cmatch '[a-z]' -and $s -cmatch '[A-Z]' -and $s -match '\d')
    $s
}
function New-Hex([int]$bytes = 32) {
    $b = New-Object byte[] $bytes
    [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
    -join ($b | ForEach-Object { $_.ToString('x2') })
}
function Ensure-Cfg([string]$k, [scriptblock]$gen) { if (-not $Script:Cfg.Contains($k)) { Set-Cfg $k (& $gen) } }

# -------------------------------------------------------------- 0. checks
Step 'Preflight'
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) { Fail 'Run this script from an elevated (Run as administrator) PowerShell.' }

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { Fail 'docker not found on PATH. Start Docker Desktop once and retry.' }
Try-Native { docker info *> $null }
if ($LASTEXITCODE -ne 0) { Fail 'Docker daemon is not reachable. Start Docker Desktop and wait until it reports "running".' }
Try-Native { docker compose version *> $null }
Assert-Native 'docker compose (v2) check'

$localIps = @(Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue | ForEach-Object { $_.IPAddress })
if ($localIps -notcontains $ServerIp) {
    Write-Warning "$ServerIp is not assigned to any adapter on this machine (found: $($localIps -join ', ')). Continuing, but the stack will not be reachable at that address until it is."
}
Info "Docker OK; server address $ServerIp"

# ------------------------------------------------------ 1. terraform
Step 'Terraform'
function Refresh-Path {
    $m = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $u = [Environment]::GetEnvironmentVariable('Path', 'User')
    $env:Path = (@($m, $u) | Where-Object { $_ }) -join ';'
}
if (-not (Get-Command terraform -ErrorAction SilentlyContinue)) {
    Info 'terraform not found; installing'
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        winget install --id Hashicorp.Terraform -e --silent --accept-source-agreements --accept-package-agreements
        Refresh-Path
    }
    if (-not (Get-Command terraform -ErrorAction SilentlyContinue)) {
        Info 'winget unavailable or did not put terraform on PATH; using the HashiCorp zip'
        $dest = 'C:\Tools\terraform'
        New-Item -ItemType Directory -Force -Path $dest | Out-Null
        $zip = Join-Path $env:TEMP "terraform_$TerraformVersion.zip"
        $url = "https://releases.hashicorp.com/terraform/$TerraformVersion/terraform_${TerraformVersion}_windows_amd64.zip"
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing
        Expand-Archive -Path $zip -DestinationPath $dest -Force
        Remove-Item $zip
        $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
        if (($machinePath -split ';') -notcontains $dest) {
            [Environment]::SetEnvironmentVariable('Path', ($machinePath.TrimEnd(';') + ';' + $dest), 'Machine')
        }
        Refresh-Path
    }
    if (-not (Get-Command terraform -ErrorAction SilentlyContinue)) { Fail 'Terraform install failed; install it manually and re-run.' }
}
Info ((terraform version | Select-Object -First 1))

# ---------------------------------------------------------- 2. secrets
Step 'Secrets and configuration'
Load-Env
# Repo path as the Docker daemon sees it (Docker Desktop WSL2 backend).
$drive = $RepoRoot.Substring(0, 1).ToLower()
$repoVm = "/run/desktop/mnt/host/$drive" + ($RepoRoot.Substring(2) -replace '\\', '/')
Set-Cfg 'SERVER_IP' $ServerIp
Set-Cfg 'REPO_ROOT' $RepoRoot
Set-Cfg 'REPO_VM' $repoVm
Set-Cfg 'WOODPECKER_PORT' '8000'
Set-Cfg 'GITEA_URL' $GiteaUrl
Set-Cfg 'WOODPECKER_URL' $WpUrl
Set-Cfg 'ADMIN_USER' 'gateadmin'
Ensure-Cfg 'POSTGRES_PASSWORD'        { New-Hex 24 }
Ensure-Cfg 'WOODPECKER_AGENT_SECRET'  { New-Hex 32 }
Ensure-Cfg 'WOODPECKER_GRPC_SECRET'   { New-Hex 32 }
Ensure-Cfg 'PORTAL_SESSION_KEY'       { New-Hex 32 }
# Placeholders satisfy compose's required-variable check until the real
# OAuth application exists (it can only be created once Gitea is running).
Ensure-Cfg 'GITEA_OAUTH_CLIENT_ID'     { 'pending' }
Ensure-Cfg 'GITEA_OAUTH_CLIENT_SECRET' { 'pending' }
$accounts = 'ADMIN', 'DEV1', 'DEV2', 'DEV3', 'BOT', 'REPORTER'
foreach ($a in $accounts) { Ensure-Cfg "${a}_PASSWORD" { New-Secret 20 } }
New-Item -ItemType Directory -Force -Path (Join-Path $StateDir 'tmp') | Out-Null
Info "State: $StateDir (git-ignored; holds secrets)"

# -------------------------------------------------------- 3. firewall
if (-not $SkipFirewall) {
    Step 'Windows Firewall (LAN ports 3500, 8000, 8181)'
    $rule = 'SSDLC UAT'
    if (-not (Get-NetFirewallRule -DisplayName $rule -ErrorAction SilentlyContinue)) {
        New-NetFirewallRule -DisplayName $rule -Direction Inbound -Action Allow -Protocol TCP `
            -LocalPort 3500, 8000, 8181 -Profile Any | Out-Null
        Info 'rule created'
    } else { Info 'rule already present' }
}

# ------------------------------------------------------ 4. terraform
Step 'Docker network and volumes (Terraform)'
$netExists = (docker network ls --filter name=^ssdlc-minimal$ --format '{{.Name}}') -eq 'ssdlc-minimal'
$tfState = Join-Path $TfDir 'terraform.tfstate'
if ((Test-Path $tfState) -and -not $netExists) {
    # A state file copied from another machine would make Terraform believe
    # the resources already exist.
    $stale = "$tfState.stale-$(Get-Date -Format yyyyMMddHHmmss)"
    Move-Item $tfState $stale
    if (Test-Path "$tfState.backup") { Move-Item "$tfState.backup" "$stale.backup" }
    Info "moved stale Terraform state aside ($stale)"
}
$dockerHost = docker context inspect --format '{{.Endpoints.docker.Host}}'
terraform "-chdir=$TfDir" init -input=false
Assert-Native 'terraform init'
# Resources left behind by an earlier (possibly failed) run exist in Docker
# but may be missing from Terraform's state; adopt them instead of failing
# with "network ... already exists".
$tracked = @(Try-Native { terraform "-chdir=$TfDir" state list 2>$null })
$adopt = [ordered]@{
    'docker_network.minimal'                = @('network', 'ssdlc-minimal')
    'docker_volume.postgres_data'           = @('volume', 'ssdlc-minimal-postgres-data')
    'docker_volume.gitea_data'              = @('volume', 'ssdlc-minimal-gitea-data')
    'docker_volume.woodpecker_server_data'  = @('volume', 'ssdlc-minimal-woodpecker-server-data')
    'docker_volume.trivy_db_cache'          = @('volume', 'ssdlc-minimal-trivy-db-cache')
}
foreach ($addr in $adopt.Keys) {
    if ($tracked -contains $addr) { continue }
    $kind, $name = $adopt[$addr]
    $id = Try-Native { docker $kind inspect --format '{{.Id}}' $name 2>$null }
    if ($LASTEXITCODE -ne 0 -or -not $id) { continue }
    if ($kind -eq 'volume') { $id = $name }
    Info "adopting existing $kind $name into Terraform state"
    terraform "-chdir=$TfDir" import -input=false -var "docker_host=$dockerHost" $addr $id
    Assert-Native "terraform import $addr"
}
terraform "-chdir=$TfDir" apply -auto-approve -input=false -var "docker_host=$dockerHost"
Assert-Native 'terraform apply'

# ------------------------------------------------------------ 5. images
Step 'Building portal and tooling images'
docker build -f $Dockerfile --target ops -t ssdlc-uat-ops $RepoRoot
Assert-Native 'ops image build'

function Compose {
    docker compose -p ssdlc-uat --env-file $EnvFile -f $BaseCompose -f $UatCompose @args
    Assert-Native "docker compose $($args -join ' ')"
}
function Ops {
    # Runs a command in the ops image with the repo mounted at the daemon's
    # own view of its path, so nested `docker run -v` bind mounts resolve.
    $vm = $Script:Cfg['REPO_VM']
    docker run --rm --env-file $EnvFile `
        -v "${RepoRoot}:${vm}" -w $vm `
        -v /var/run/docker.sock:/var/run/docker.sock `
        -e "TMPDIR=$vm/deploy/uat/state/tmp" `
        ssdlc-uat-ops @args
    Assert-Native "ops: $($args -join ' ')"
}
function Wait-Http([string]$url, [int]$seconds = 240) {
    $deadline = (Get-Date).AddSeconds($seconds)
    while ((Get-Date) -lt $deadline) {
        try { $r = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 5; if ($r.StatusCode -lt 500) { return } } catch { }
        Start-Sleep -Seconds 3
    }
    Fail "Timed out waiting for $url"
}

# ------------------------------------------- 6. gitea, accounts, oauth
Step 'Starting Postgres and Gitea'
Compose up -d postgres gitea
Wait-Http "$GiteaUrl/api/healthz"

function Gitea-Cli { docker exec -u git ssdlc-minimal-gitea gitea @args }
function Ensure-GiteaUser([string]$name, [string]$pwKey, [switch]$Admin, [switch]$ForceChange) {
    $exists = Try-Native { Gitea-Cli admin user list 2>$null } | Select-String -Pattern "\s$([regex]::Escape($name))\s" -Quiet
    if ($exists) { Info "user $name exists"; return }
    $args2 = @('admin', 'user', 'create', '--username', $name, '--email', "$name@ssdlc.local",
               '--password', $Script:Cfg[$pwKey], "--must-change-password=$($ForceChange.IsPresent.ToString().ToLower())")
    if ($Admin) { $args2 += '--admin' }
    Gitea-Cli @args2 | Out-Null
    Assert-Native "create user $name"
    Info "user $name created"
}
Step 'Creating accounts'
Ensure-GiteaUser 'gateadmin'     'ADMIN_PASSWORD' -Admin
Ensure-GiteaUser 'dev1'          'DEV1_PASSWORD' -ForceChange
Ensure-GiteaUser 'dev2'          'DEV2_PASSWORD' -ForceChange
Ensure-GiteaUser 'dev3'          'DEV3_PASSWORD' -ForceChange
Ensure-GiteaUser 'gate-bot'      'BOT_PASSWORD'
Ensure-GiteaUser 'gate-reporter' 'REPORTER_PASSWORD'

function New-GiteaToken([string]$user, [string]$scopes) {
    $name = "uat-$(Get-Date -Format yyyyMMddHHmmss)"
    $t = (Gitea-Cli admin user generate-access-token --username $user --token-name $name --scopes $scopes --raw) | Select-Object -Last 1
    Assert-Native "token for $user"
    $t.Trim()
}
Ensure-Cfg 'GITEA_ADMIN_TOKEN' { New-GiteaToken 'gateadmin' 'all' }
Ensure-Cfg 'GITEA_BOT_TOKEN'   { New-GiteaToken 'gate-bot' 'write:repository,read:user' }
Ensure-Cfg 'REPORTER_TOKEN'    { New-GiteaToken 'gate-reporter' 'write:repository,write:issue,read:user' }

Step 'Organisation, teams, repositories, OAuth applications'
Merge-Output (Ops python deploy/uat/bootstrap.py gitea)

# --------------------------------------------- 7. woodpecker + the rest
Step 'Starting Woodpecker, DAST target'
Compose up -d postgres gitea woodpecker-server woodpecker-agent staging-target
Wait-Http "$WpUrl/healthz"

Step 'Woodpecker API token for the admin'
if (-not $Script:Cfg.Contains('WOODPECKER_TOKEN')) {
    Merge-Output (Ops python deploy/uat/bootstrap.py wp-token)
}

Step 'Onboarding the pilot repository ssdlc/pilot-app (pipeline, baseline, branch protection)'
Ops sh scripts/onboard-repo.sh ssdlc pilot-app
Merge-Output (Ops python deploy/uat/bootstrap.py repo-id)
Ops python deploy/uat/bootstrap.py wp-secrets

Step 'Starting the portal and the bot approver'
Compose up -d --build portal bot-approver

# ------------------------------------------------------ 8. credentials
$cfg = $Script:Cfg
$creds = @"
SSDLC PLATFORM - UAT CREDENTIALS
Generated $(Get-Date -Format 'yyyy-MM-dd HH:mm')   KEEP THIS FILE PRIVATE

Gitea (code + PRs) : $GiteaUrl
Woodpecker (CI)    : $WpUrl       (sign in with "Gitea" and the Gitea account)
Portal             : $PortalUrl   (sign in with Gitea)
Pilot repository   : $GiteaUrl/ssdlc/pilot-app

Account     Role                       Password
gateadmin   Administrator, approver    $($cfg['ADMIN_PASSWORD'])
dev1        Developer, approver        $($cfg['DEV1_PASSWORD'])
dev2        Developer                  $($cfg['DEV2_PASSWORD'])
dev3        Developer                  $($cfg['DEV3_PASSWORD'])

Developers are asked to change their password on first login.

Service accounts (not for people): gate-bot / gate-reporter - passwords in state\uat.env
"@
[IO.File]::WriteAllText($CredFile, $creds, (New-Object Text.UTF8Encoding $false))

Step 'Done'
Write-Host $creds
Write-Host "Credentials saved to $CredFile"
Write-Host 'Next: see deploy\uat\README.md ("First checks").'
