# Pinned versions

Single source of truth for every version pinned into compose files, Ansible roles, and pipeline
templates. **Checked against each project's actual GitHub Releases API on 2026-08-23** — not
assumed, not carried over from an earlier research pass. Re-verify before each Milestone; a tool
that goes stale here is a tool nobody is patching (see DESIGN.md's "0.25–0.5 FTE standing" cost).

Pin by **exact version**, never a floating tag (`latest`, `1`, `3`) — floating tags are how a
Tuesday-afternoon upstream release becomes an unreviewed change to what the gate enforces.

## Core platform

**Re-checked 2026-08-28** (SADR-0026, self-scan-driven — the first live vulnerability scan of the
platform's own deployed images, not just its own code).

| Component | Pinned | Released | Note |
|---|---|---|---|
| Gitea | **1.27.2** | 2026-08-13 | Re-confirmed 2026-08-28 as still the latest release — nothing to bump. See [PINNED_VERSIONS §Gitea CVE posture](#gitea-cve-posture-as-of-2026-08-23) below — do not pin without reading it |
| Woodpecker CI | **v3.18.0** | 2026-08-24 | Bumped from v3.17.0 (SADR-0026). Server and agent must stay in lockstep — v3.18 enforces stricter gRPC protocol-version matching between them. First boot alters log storage (migration duration scales with stored log volume, trivial for a lab-scale deployment). Renamed `WOODPECKER_GRPC_VERIFY` → `WOODPECKER_GRPC_SKIP_VERIFY`; confirmed unused here |
| postgres (compose base image) | **18.6-alpine** | — | Bumped from `postgres:16-alpine` (SADR-0026) — that pin was itself a **floating tag**, in violation of this file's own rule below; re-pinned to the exact patch this time. Major-version jump: needs a real `pg_dumpall`/restore migration, not a bare image swap, and 18+'s official image changed its expected volume-mount layout (`docker-library/postgres#1259`) — see the SADR before touching this again |
| gitea-mq | *commit SHA, not a tag* | pushed 2026-08-20 | **Zero tagged releases exist.** Actively developed (27★, MIT) but unversioned — pin the exact commit SHA used, re-evaluate before Milestone 3 makes it the primary merge-queue path rather than the sidecar fallback |

## Scanners (fast + deep gates)

| Component | Pinned | Released | Note |
|---|---|---|---|
| Semgrep OSS | **v1.174.0** | 2026-08-20 | Rules vendored separately — see DESIGN.md, rule-set updates |
| Gitleaks | **v8.30.1** | 2026-03-21 | ~5 months since last tag — check for an interim security fix before Milestone 1 |
| Trivy | **v0.74.0** | 2026-08-14 | DB refreshed out-of-band per DESIGN.md; binary itself pinned here |
| Checkov | **3.3.13** | 2026-08-20 | |
| Syft | **v1.51.0** | 2026-08-10 | |
| OWASP ZAP | **v2.17.0** (Docker image: `zaproxy/zap-stable:latest`) | 2025-12-15 | Tag is 8 months old; repo itself pushed 2026-08-21 (857 open issues) — actively developed between releases. Confirm no interim CVE fix before relying on this for the scheduled DAST baseline. **Image name corrected in [SADR-0006](adr/0006-dast-zap-juice-shop.md)** — `zaproxy/zaproxy:stable` does not exist (returns "access denied", easily misread as an auth problem); the current repo is `zaproxy/zap-stable` (or `ghcr.io/zaproxy/zaproxy:stable`), and the older `owasp/zap2docker-stable` is deprecated |
| Conftest (OPA) | **v0.69.0** | 2026-08-03 | Bundles OPA v1.19.0 |
| Cosign | **v3.1.3** | 2026-08-06 | |

## PR integration & dependency automation

| Component | Pinned | Released | Note |
|---|---|---|---|
| reviewdog | **v0.21.0** | 2025-09-03 | ~1 year since last tag; repo pushed 2026-08-22 (137 open issues) — active but stable. Re-check for security-relevant commits since tag before Milestone 2 |
| Renovate | **44.39.3** | 2026-08-23 | Continuous versioning, ships constantly — this pin is same-day and will be stale within the week; treat as a floor, re-pin at each Ansible run |

## Findings, SBOM, and reporting

| Component | Pinned | Released | Note |
|---|---|---|---|
| DefectDojo | **3.2.201** | 2026-08-18 | |
| Dependency-Track | **5.0.4** | 2026-07-30 | |

## Kubernetes / admission / runtime (K8s profile only)

| Component | Pinned | Released | Note |
|---|---|---|---|
| Kyverno | **v1.19.0** | 2026-08-20 | |
| Falco | **0.44.1** | 2026-06-11 | |
| OWASP CoreRuleSet (WAF) | **v4.29.0** | 2026-08-17 | |

## Observability

| Component | Pinned | Released | Note |
|---|---|---|---|
| Prometheus | **v3.14.0** | 2026-08-18 | |
| Grafana | **v13.2.0** | 2026-08-18 | Ships a fix for CVE-2026-17183 in this same release — pin at or above |
| Loki | **v3.7.6** | 2026-08-06 | |

## Developer tooling

| Component | Pinned | Released | Note |
|---|---|---|---|
| pre-commit | **v4.6.2** | 2026-08-10 | |
| Checkov | **3.3.13** | 2026-08-20 | Duplicate of the fast-gate scanner row above — same tool, listed once for the pipeline, once here since it's also run locally against this project's own IaC (D6 dogfooding). See [SADR-0010](adr/0010-terraform-docker-resources.md): it has **zero built-in policies for the `docker` Terraform provider's resource types** — a clean run against `terraform/local/` proves nothing about their security, only that Checkov has nothing to say about them yet |

## Infrastructure as Code (control-plane tooling, not container images)

| Component | Pinned | Released | Note |
|---|---|---|---|
| Terraform CLI | **v1.15.9** | 2026-08-19 | Runs natively on Windows — confirmed live. Installed as a standalone binary here (not via a system package manager), since this host has no admin-elevated install path assumed |
| kreuzwerker/docker (Terraform provider) | **v4.5.0** | 2026-06-18 | `terraform/local/` only, the docker-daemon-level slice — see [SADR-0010](adr/0010-terraform-docker-resources.md) |
| Ansible | *not installed — see note* | — | **Cannot run as a control node on native Windows in this environment.** Confirmed live: `ansible-playbook --version` raises `OSError: [WinError 87]` from `ansible/cli/__init__.py`'s `check_blocking_io()`, which calls `os.get_blocking()` — unsupported on Windows file descriptors. This is Ansible's own documented control-node platform requirement, not a missing dependency; pip-installs the package fine, just cannot execute. `ansible/` playbooks in this repo are **written, not live-tested** until run from a real POSIX control node (WSL2 with a real distro, a Linux CI runner, or macOS) — see SADR-0010's decision section before trusting them un-reviewed |

---

## Gitea CVE posture, as of 2026-08-23

Pulled directly from the GitHub Security Advisories API for `go-gitea/gitea` (`gh api
repos/go-gitea/gitea/security-advisories`, 84 advisories total, spanning 2026-04-18 through
2026-08-15) — not summarised secondhand.

**Latest stable is `1.27.2`** (released 2026-08-13). A batch of **seven advisories published
2026-08-14** — the day *after* `1.27.2` shipped — lists `vulnerable_range: <= 1.27.1` for most
entries, with no explicit `first_patched_version` populated by GitHub's API. Following normal
coordinated-disclosure timing (patch ships, advisory follows within a day or two), it is reasonable
to infer `1.27.2` is the fix — but this is an **inference, not a confirmed statement**, and should
be re-verified against Gitea's own CHANGELOG.md at the pinned tag before Milestone 1 hardens on it.

**The two advisories that affect design decisions already made**, not just the version pin:

- **CVE-2026-58439 / GHSA-w5pg-649r-p6gg** — PR-retargeting approval bypass. Directly relevant to
  D2's `required_approvals: 2` control. See SADR-0001's follow-up section — **not yet independently
  tested**, tracked as SADR-0003 before Milestone 3 trusts it.
- **CVE-2026-73278 / CVE-2026-73535** — WebAuthn/TOTP second factor bypassed on the OAuth2/OpenID
  sign-in path (two related, separately-numbered advisories). Directly relevant to **D10 / open
  question 4** (identity source) and the Entra ID SSO recommendation in the integration menu — if
  Entra ID is wired up via OIDC, this exact class of bug is what to test for before trusting that
  2FA is enforced on the SSO path, not just on password login.

**Two RCEs, both mitigated by a configuration choice already implied by the design, now made
explicit**:

- **CVE-2026-73539** (RCE via external markup renderer argv injection) and **CVE-2026-59774**
  (unauthenticated arbitrary file read → RCE via the `go-org` renderer, critical, affects
  `1.22.1–1.27.0`) both require an **optional external/markup renderer** (`pandoc`, org-mode) to be
  configured. **Hardening rule for D6: do not enable `[markup.*]` external renderers at all**,
  unless a specific one is vetted and the need is concrete. `1.27.2` postdates the `go-org` CVE's
  stated affected range (`<= 1.27.0`) entirely.

**One advisory that further validates D8** (Woodpecker over Gitea Actions): **CVE-2026-73802**
(critical, 2026-08-15) is a privilege-escalation bug in `gitea-runner`, the Gitea Actions execution
agent — irrelevant to this platform, since D8 already chose not to run Gitea Actions at all.

**One advisory extending D2's audit guidance**: **CVE-2026-73814** (medium) — an Admin-level repo
collaborator (not just Owner) can self-escalate to Owner and transfer the repo. D2 already says
"audit PATs for broad `write:repository` scope" — extend that audit to **any Admin-level
collaborator grant**, not just token scope.

**Overall signal:** 84 advisories in a four-month window is a high patch cadence. This reinforces
DESIGN.md D6's "self-patch SLA tighter than the one imposed on product teams" — it is not a
one-time pin-and-forget; re-running this exact `gh api` query is a standing operational task, not a
Milestone-0 checkbox.

## Re-verification cadence

This file is stale the day after it's written — several of the entries above say so explicitly.
Treat it as a **floor**, checked at each milestone boundary, not a document to trust indefinitely.
The command that produced the Gitea advisory data:

```
gh api repos/go-gitea/gitea/security-advisories --paginate > gitea_advisories.json
```

Re-run the same pattern (`gh api repos/<owner>/<repo>/releases/latest`) for any component before
pinning it into a new environment.
