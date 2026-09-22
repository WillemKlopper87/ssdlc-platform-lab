#Requires -Version 5.1
<#
.SYNOPSIS  Stop the UAT stack. With -Wipe, also delete ALL UAT data (repos, users, CI history).
#>
[CmdletBinding()]
param([switch]$Wipe)
$ErrorActionPreference = 'Stop'
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$EnvFile  = Join-Path $PSScriptRoot 'state\uat.env'
if (-not (Test-Path $EnvFile)) { throw 'No state\uat.env - nothing to tear down.' }
docker compose -p ssdlc-uat --env-file $EnvFile `
    -f (Join-Path $RepoRoot 'compose\minimal\docker-compose.yml') `
    -f (Join-Path $PSScriptRoot 'docker-compose.uat.yml') down
if ($Wipe) {
    $ans = Read-Host 'This DELETES all Gitea/Woodpecker/Postgres data and the generated credentials. Type WIPE to confirm'
    if ($ans -ne 'WIPE') { throw 'Cancelled.' }
    terraform "-chdir=$(Join-Path $RepoRoot 'terraform\local')" destroy -auto-approve
    docker volume rm ssdlc-minimal-trivy-db-cache 2>$null
    Remove-Item -Recurse -Force (Join-Path $PSScriptRoot 'state')
    Write-Host 'Wiped. Re-run setup-uat.ps1 for a clean deployment.'
}
