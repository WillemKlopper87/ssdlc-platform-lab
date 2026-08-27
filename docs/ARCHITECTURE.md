# Architecture

Components, data flow, and trust boundaries as actually built and live-tested — not the target
design. See [`DESIGN.md`](DESIGN.md) for the full target architecture and the decisions (D1–D10)
behind it; this document describes what exists in `compose/minimal` today and where it stops.

## Components, current state

| Component | Status | Notes |
|---|---|---|
| Gitea (1.27.2) | Live | System of record for source, identity, branch protection, review state. |
| Woodpecker CI (v3.17.0) | Live | Server + one agent, Docker backend. Reports commit status to Gitea natively (D1). |
| Postgres | Live | Backs both Gitea and Woodpecker (`init-multi-db.sh` creates two databases). |
| `policy-eval/evaluate-findings.py` | Live | Runs as a Woodpecker step (`policy-eval-findings`) in every onboarded repo's own pipeline. |
| `policy-eval/verify-approvals.py` | Live | Runs as `approval-check`, same pipeline. |
| `normalise/*_adapter.py` | Live | Gitleaks, Semgrep, Trivy — see [`POLICY.md`](POLICY.md). |
| `policy/severity.rego` | Live | Evaluated by Conftest, invoked from `evaluate-findings.py`. |
| `scripts/bot-approver.py` | Live, manual | Standing poll loop; not yet a `compose/minimal` service — has to be started by hand. Single-repo per process. |
| `scripts/onboard-repo.sh` | Live | Platform-admin, one-shot per repo. |
| `scripts/reconciliation-loop.py` | Live | Recovers PRs stuck with no gate verdict (SADR-0015). Not yet a standing service either. |
| `scripts/trusted-gate-runner.py` + `gate-bundle/` | Built, not deployed | SADR-0017's durable trust boundary. Designed and unit-tested; no isolated runner host exists in this environment to deploy it to. |
| Reporting sidecar | Not built | D1's "reporting, not trust-path" service — sticky comments, DefectDojo push, metrics, `/exception` commands, reconciliation loop as a standing process. Currently these exist only as standalone scripts run manually or not at all. |
| Exceptions repo | Not built | D5's signed exception/baseline store. No baseline gating exists yet — every finding blocks regardless of whether it predates onboarding. See [`EXCEPTIONS.md`](EXCEPTIONS.md). |
| DefectDojo, Dependency-Track | Not built | Deferred past the trusted-pilot phase (`FRAMEWORK-ADDENDUM-2026.md`). |
| Portal (D10) | Not built | Milestone 8. |

## Current data flow — a push or PR on an onboarded repo

```
developer push/PR
      │
      ▼
Gitea (webhook fires — see D3: keyed to the SHA, cancels superseded runs)
      │
      ▼
Woodpecker runs .woodpecker.yml (pipelines/fast.woodpecker.yml, committed
into the repo by onboard-repo.sh, not written by the developer)
      │
      ├─ secrets      (Gitleaks)      → gitleaks-report.json
      ├─ sast         (Semgrep)       → semgrep-report.json / .sarif
      ├─ dependencies (Trivy)         → trivy-report.json
      ├─ approval-check                → policy-eval/verify-approvals.py
      │     re-derives approval validity against the CURRENT base and head
      │     SHA independently of Gitea's stored `official` flag (SADR-0003,
      │     SADR-0009); requires the gate bot's current-head approval plus a
      │     distinct non-author human when GATE_BOT_LOGIN is set (SADR-0013).
      ├─ inline-comments                → reviewdog, Semgrep SARIF only, non-blocking
      ├─ policy-eval-findings           → normalise/*_adapter.py → policy/severity.rego
      │     via Conftest → exit 0/1/2. THE EXIT CODE IS THE GATE (SADR-0018).
      └─ summary
      │
      ▼
Woodpecker reports commit status to Gitea natively
(context: ssdlc/security-gate/<event>/<workflow> — glob-matched in branch
protection, not an exact string; see DESIGN.md D1's correction)
      │
      ▼
scripts/bot-approver.py (separate process, polling)
      compares every gate-managed file (the pipeline, evaluator, adapters,
      policy) at PR head against the protected base via Gitea blob SHAs
      (GATE_CONTRACT_ENFORCE=1, SADR-0017) — a mismatch blocks the bot's
      vote outright, regardless of scan results
      → casts the bot's approval once scanning steps are green and the
        gate-managed files are unchanged
      │
      ▼
Gitea branch protection: required_approvals: 2 (bot + a distinct human),
dismiss_stale_approvals: true, status check glob-matched — merge only when
all three hold for the current head
```

## Trust boundaries

**In the trust path** (a compromise here can pass an unreviewed or unscanned PR):
- `policy-eval/evaluate-findings.py`, `policy-eval/verify-approvals.py`, `normalise/*_adapter.py`,
  `policy/severity.rego`, `pipelines/fast.woodpecker.yml` — everything `scripts/bot-approver.py`'s
  `GATE_CONTRACT_ENFORCE` check treats as gate-managed and compares against the protected base.
- The gate bot's Gitea token (`GITEA_BOT_TOKEN`) — D2's "crown jewel." Held only by
  `bot-approver.py`'s own process, never by a build container.
- `GITEA_ADMIN_TOKEN` — holds a standing push-whitelist bypass of PR review on every onboarded repo
  (`onboard-repo.sh`'s own comment on this is explicit). Scoped to the platform-admin's own use, not
  a service credential to hand out.

**Currently in the trust path but architecturally shouldn't be, per SADR-0017 — the open gap Sprint 01
exists to close:** the scanning steps themselves. `pipelines/fast.woodpecker.yml` and everything it
depends on is committed *into* the onboarded repo by `onboard-repo.sh` and executed by a Woodpecker
agent that also runs the untrusted PR's own build steps in the same container-per-step model. The
`GATE_CONTRACT_ENFORCE` byte-comparison against the protected base is a real, live-tested mitigation
(a PR cannot silently swap in a weakened evaluator and get the bot's vote), but it is a **transitional
control**, not the durable design. The durable design — `scripts/trusted-gate-runner.py` executing
`gate-bundle/`'s pinned, signed image outside the application's own CI context entirely — is built and
unit-tested but not deployed; see [`GATE_CONTRACT.md`](GATE_CONTRACT.md).

**Outside the trust path** (a compromise degrades developer experience, not the gate's integrity):
- `inline-comments` (reviewdog) — posts PR comments, never blocks.
- `scripts/reconciliation-loop.py` — D4: "it cannot make a red PR green, only un-stick a PR that
  never got a verdict at all." Live-tested against this exact boundary in SADR-0015.
- The (not-yet-built) reporting sidecar, portal, DefectDojo/Dependency-Track integrations.

## What "minimal" currently omits versus DESIGN.md's target

- No baseline/differential gating — see [`EXCEPTIONS.md`](EXCEPTIONS.md). Every finding blocks
  regardless of history, which DESIGN.md's *Policy model* names as "the rollout killer neither
  earlier review caught." Acceptable only because Sprint 01 restricts onboarding to pilot repos with
  no pre-existing findings, not a general-purpose posture yet.
- No pre-receive secret-detection hook wired into `onboard-repo.sh` (SADR-0002 built and tested the
  mechanism; it is not yet packaged as the Ansible role that installs it on every onboarded repo).
- No standing services for `bot-approver.py` or `reconciliation-loop.py` — both are scripts run by
  hand today, not `compose/minimal` entries.
- Prometheus/Grafana, evidence export, and the exceptions repo do not exist in this profile.

## Reference

Full component inventory, reuse-vs-build decisions, and the target architecture diagram: `DESIGN.md`
§Architecture and §Component inventory. Every claim above traces to a specific SADR under `docs/adr/`
— check the SADR before trusting a line here that seems to contradict the running system, since this
file (like `PINNED_VERSIONS.md`) decays and should be re-checked at each milestone boundary.
