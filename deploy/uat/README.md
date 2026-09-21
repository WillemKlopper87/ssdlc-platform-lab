# SSDLC platform - UAT deployment (Windows + Docker Desktop)

Target: one Windows server (LAN IP auto-detected; re-run the script if it changes), 3 developers + 1 admin.
Ollama / Continue is **not** deployed (it never existed in this repo: "designed, not built").

## Deploy

1. Copy this whole worktree (`ssdlc-portal`, it contains the portal) to the server.
   Docker Desktop must be running. Internet access is needed for image pulls and the Terraform Docker provider.
2. Elevated PowerShell:
   ```powershell
   Set-ExecutionPolicy -Scope Process Bypass
   .\deploy\uat\setup-uat.ps1 -ServerIp 192.168.1.28
   ```
   The script installs Terraform if missing (winget, else the HashiCorp zip into `C:\Tools\terraform`; added to the machine PATH),
   opens TCP 3500/8000/8181 in Windows Firewall, and is safe to re-run.
3. Credentials are printed and saved to `deploy\uat\state\credentials.txt` (git-ignored).

| URL | What |
|---|---|
| http://192.168.1.28:3500 | Gitea - code, PRs, reviews |
| http://192.168.1.28:8000 | Woodpecker - CI (sign in with Gitea) |
| http://192.168.1.28:8181 | Portal - dashboard, PR security report, exceptions, onboarding |

Accounts: `gateadmin` (admin, approver), `dev1` (developer, approver), `dev2`, `dev3`. Developers must change their password at first login.
Pilot repo `ssdlc/pilot-app` is onboarded (gate pipeline, baseline, branch protection: 2 approvals = `gate-bot` + one non-author human).

## First checks

1. `dev2` clones `http://192.168.1.28:3500/ssdlc/pilot-app.git`, pushes a branch, opens a PR.
2. The pipeline runs in Woodpecker; the PR shows the `ssdlc/security-gate` status.
3. `gate-bot` approves within ~15 s of a green scan; `dev1` or `gateadmin` approves as the human; merge.
4. Portal: sign in with Gitea, open Dashboard and the PR report.

## Operations

- Stop: `.\deploy\uat\teardown-uat.ps1`; delete all data: `.\deploy\uat\teardown-uat.ps1 -Wipe`.
- More repos: onboard from the portal *Onboarding* page (approvers only). The bot approver is currently wired to
  `ssdlc/pilot-app` only (`bot-approver.py` watches one repo per process); a second repo needs a second bot container.
- **Reporting service** (`ssdlc-uat-sidecar`): posts one edited-in-place comment per PR, serves report data and
  Prometheus metrics on port 8282 (internal only, not published), and reports dependency health. It is never in
  the merge decision: stopping it changes nothing about which PRs pass or block. Logs: `docker logs ssdlc-uat-sidecar`.
- Secrets live in `deploy\uat\state\uat.env`. Treat it like a password file.

## Known limitations (from docs/SSDLC_UAT_Readiness_Report.pdf)

- Exception workflow: B3 (second approver refused), B7 (approved exceptions not consulted by the pipeline) are open engineering items.
- Gate runs in "transitional" mode (Woodpecker pipeline + protected-file comparison), not the signed platform-owned bundle (B8).
- DefectDojo / Dependency-Track / Renovate / Loki are not built in this repo.
- Woodpecker is set to `OPEN=true`: any existing Gitea user may sign in. Gitea registration is disabled, so that is the 4 accounts.
- Scheduled jobs (`trivy-db-refresh`, DAST cron) are not registered.
- Images use the project's pinned versions except the tooling image (`python:3.12-slim`, `requests==2.32.3`), which are not digest-pinned.
