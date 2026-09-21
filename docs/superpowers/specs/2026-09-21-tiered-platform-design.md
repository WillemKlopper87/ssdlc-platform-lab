# Tiered platform: reporting service, posture, DefectDojo, export, health

**Status:** draft for review
**Implements:** the unbuilt parts of [`DESIGN.md`](../../DESIGN.md) D10 and its `minimal`/`full` profiles, for the UAT deployment in `deploy/uat/`.
**Builds on:** [`2026-09-07-ssdlc-portal-design.md`](2026-09-07-ssdlc-portal-design.md) (the four built screens and the greyed-out ones this closes).

## Goal

Deliver the three greyed-out portal screens (Report export, Posture, Admin health) and the components behind them (reporting service, Prometheus + Grafana, DefectDojo) without assuming the server is big. The UAT server is small today and a larger one is coming, so the same setup script must **measure the memory Docker can use and start only what fits**.

Non-goals: Dependency-Track, Renovate, Loki, Ollama, the exception bugs B1-B7, the signed gate bundle (B8). Each is a separate piece of work.

## Invariants (from DESIGN.md, not negotiable here)

1. **Nothing added here is in the merge-decision path.** The gate is computed from scanner output, policy and local state only. If the reporting service, Grafana or DefectDojo is down, PRs still pass or block correctly.
2. **The portal owns no security state.** It reads from Gitea, Woodpecker, the reporting service and (optionally) DefectDojo.
3. **Build containers never hold new tokens.** DefectDojo and Gitea-comment credentials live only in the reporting service.

## Tiers

The setup script reads `docker info --format '{{.MemTotal}}'` (the memory the Docker VM can actually use, which on Docker Desktop is the WSL2 limit, not the Windows RAM) and picks a tier. `-Tier core|standard|full` overrides it. The choice and its reason are printed and written to `state/uat.env` as `TIER`.

| Tier | Docker-usable memory | Runs in addition to today's stack |
|---|---|---|
| `core` | under 6 GB | reporting service; portal report export and Admin health |
| `standard` | 6 to under 12 GB | + Prometheus, Grafana (Posture screen) |
| `full` | 12 GB or more | + DefectDojo (findings history, risk register) |

Thresholds are starting estimates from DESIGN.md sizing. They are confirmed by measuring `docker stats` on the real small server before they are frozen (see Open questions).

**Mechanism.** Compose profiles (`posture`, `dojo`) in a new overlay `deploy/uat/docker-compose.tiers.yml`. `core` starts no profile; `standard` enables `posture`; `full` enables `posture` and `dojo`. Memory limits (`mem_limit`) are set per service so one container cannot starve the rest.

**Graceful degradation.** The setup script passes `PORTAL_FEATURES` (comma list of `report,posture,dojo`) to the portal. A screen whose feature is absent shows "Not available on this tier (needs N GB)" rather than an error. Sidebar entries stay visible, as the portal spec requires.

**Changing tier later.** Re-running the script re-detects and reconciles: it starts newly-fitting services and stops those that no longer fit (data volumes are kept).

## Components

### 1. Reporting service (`portal/cmd/sidecar`, Go, stateless)

A second binary in the existing `ssdlc-portal` Go module, so it reuses `giteaclient`, `woodpeckerclient` and `findings` without a new module. Container image ~30 MB, runs at every tier.

It does four things:

- **Report data.** For a PR it assembles gate state, findings and their explanations from Woodpecker's step logs (the parser the portal already uses) and serves it as JSON at `GET /api/v1/reports/{owner}/{repo}/{pr}`. The portal's PR report page switches to this API so there is one implementation.
- **Metrics.** `GET /metrics` in Prometheus format: PR gate outcomes by repo and result, blocking-findings count by severity and tool, pipeline duration, time from PR open to first verdict. It rebuilds them from Gitea and Woodpecker on start and refreshes on a poll (default 60 s), so it holds no state that can be lost.
- **Sticky PR comment.** One comment per PR, edited in place, summarising the gate result and linking the portal report. Uses a dedicated `gate-reporter` token (already created by setup). Best effort: failure is logged and retried, never surfaced as a gate failure.
- **DefectDojo push** (tier `full` only). See component 3.

`GET /healthz` returns the service's own status. `GET /api/v1/health` returns the dependency checks used by Admin health (below).

### 2. Posture (tier `standard` and up)

- **Prometheus** scrapes the reporting service only. Retention capped (default 15 days) to bound memory and disk.
- **Grafana** with provisioned datasource and one provisioned dashboard: gate pass rate, blocking findings by severity, average time to verdict, per-repo open findings. Provisioned as files in `deploy/uat/grafana/`, not configured by hand.
- **Portal Posture screen** embeds the dashboard. Grafana runs with anonymous Viewer access and embedding allowed, bound to the LAN, and is read-only. This is acceptable for a closed UAT LAN and is called out in the README; SSO for Grafana is deferred.

### 3. DefectDojo (tier `full`)

- Official images: Django, nginx, Celery worker + beat, Postgres, Redis/Valkey, in their own profile with their own Postgres (separate from Gitea's so a Dojo restart cannot affect Gitea).
- Setup initialises it: admin account (random password added to the credentials file), one Product per onboarded repo.
- **Import is done by the reporting service, not the pipeline**, using each tool's native parser (Semgrep JSON, Trivy JSON, Gitleaks JSON). It never sends a merged SARIF blob (DefectDojo rejects mixed tool names in one Test).
- **Asynchronous and best-effort.** A failed import is retried with backoff and never affects the gate. DefectDojo can be stopped and the next pipeline run behaves identically (the DESIGN.md D5 proof).
- The portal shows a "findings history" link per repo only when the feature is on.

### 4. Report export (all tiers; it only needs the reporting service)

`GET /reports/{owner}/{repo}/{pr}/export` in the portal returns a self-contained HTML report (findings, policy version, scanner versions, commit SHA, timestamp). It carries an HMAC-SHA256 signature over the canonical content, using a key held only by the reporting service. A `verify` page accepts an exported file and confirms the signature. PDF is deferred (see Open questions).

### 5. Admin health (all tiers)

Portal page, approvers only. The reporting service checks Gitea, Woodpecker server and agent, Postgres, the hairpin container, and whichever optional services the tier includes, and returns status, latency and a one-line detail. The page also shows the detected tier, Docker-usable memory, and current per-container memory from the Docker API. It reuses the logic of `scripts/doctor.sh` where it applies.

## Data flow

```
push/PR -> Gitea -> webhook -> Woodpecker -> scanners -> gate verdict -> Gitea status   (unchanged, authoritative)
                                     |
                       reporting service polls Gitea + Woodpecker (read-only)
                          |            |              |
                    sticky comment  /metrics      DefectDojo import (full only)
                                       |
                                  Prometheus -> Grafana -> portal Posture (standard+)
portal -> reporting service API -> PR report, export, Admin health
```

## Failure behaviour

| Failure | Effect |
|---|---|
| Reporting service down | Portal PR report and Admin health show "service unavailable"; sticky comments pause; gate unaffected |
| Prometheus or Grafana down | Posture screen shows unavailable; nothing else affected |
| DefectDojo down | Imports queue and retry; nothing else affected |
| Tier detection cannot read Docker memory | Script stops and asks for `-Tier`; it never guesses upward |

## Testing

- **Unit (Go):** metrics computation, report assembly from fixture Woodpecker logs, HMAC sign/verify, DefectDojo client against a fake server, feature-flag rendering in the portal templates.
- **Unit (PowerShell):** tier selection from injected memory values at each threshold and the override, including boundary values.
- **Live, per tier on this dev machine:** run the script with `-Tier core`, `standard` and `full` and confirm which containers exist; kill each optional service and confirm the gate result on a test PR is unchanged; export a report and verify it; tamper with an exported file and confirm verification fails.
- **Memory:** record `docker stats` for each tier under one running pipeline and publish the table in the README, replacing the estimates above.

## Delivery order

Each step gets its own implementation plan and is verified before the next starts.

1. Reporting service (report API, metrics, sticky comment, health) and portal switching to it.
2. Tier detection, compose profiles and memory limits; Prometheus, Grafana, Posture screen.
3. DefectDojo tier and import.
4. Report export and Admin health screens.

## Open questions

- **Tier thresholds.** 6 GB and 12 GB are estimates. Measure on the small server and adjust before freezing.
- **PDF export.** Needs a headless renderer (extra ~200 MB). Proposed: HTML only now; add PDF only if UAT asks for it.
- **Grafana access.** Anonymous viewer on the LAN for UAT. Gitea SSO for Grafana is possible later.
- **Which repos the reporting service watches.** Proposed: every repo in the `ssdlc` org that is active in Woodpecker, discovered on each poll, which also removes the current one-repo limit of the bot approver's wiring for reporting purposes (not for approvals).
