# Threat model — the lab itself

This is not a threat model of code the platform scans. It is a threat model of **the platform as a
target** — the same discipline D6 demands ("the platform patches itself first") applied to its own
design, not just its dependencies. Every row below traces to a specific SADR where the threat was
either confirmed by live experiment, mitigated and re-tested, or is tracked honestly as open. Where a
threat has not been tested live, this document says so — do not read an "Open" row as theoretical
comfort.

## Confirmed, then closed

| # | Threat | Confirmed by | Mitigation | Status |
|---|---|---|---|---|
| T1 | Any write collaborator can forge the gate's exact commit status and merge an unreviewed, unscanned PR — `POST /statuses/{sha}` is a repo-write operation with no binding between context name and authorised reporter | [SADR-0001](adr/0001-commit-status-forgery.md), live against Gitea 1.26: a plain `write:repository` token forged `ssdlc/security-gate` and merged | Identity-bound approval (D2): bot-only approver team, `required_approvals: 2`, forks disabled. Status alone is no longer sufficient — see T2 for the mitigation's own follow-on gap | **Closed** by D2, itself dependent on T2/T3 not reopening it |
| T2 | An approval given against an unprotected branch survives a retarget to a protected one, satisfying `required_approvals` without the approver ever reviewing the code as it lands (CVE-2026-58439) | [SADR-0003](adr/0003-pr-retargeting-approval-bypass.md), live: retarget confirmed, and `dismiss_stale_approvals` directly tested and confirmed **not** to help (it only reacts to new commits, not to the base changing) | `verify-approvals.py` independently re-derives approval validity against the PR's *current* base and reads timeline `change_target_branch` events, rather than trusting Gitea's stored `official` flag | **Closed**, unit-tested (`test_policy_eval.py`) and live |
| T3 | A pull request supplies the pipeline, policy, or approval-evaluation code that then approves its own merge — a successful pipeline status proves nothing if the PR controls what "successful" means | [SADR-0017](adr/0017-trusted-gate-contract.md) names this as the central open question Sprint 01 exists to close | Sprint 01 transitional: `bot-approver.py`'s `GATE_CONTRACT_ENFORCE` byte-compares every gate-managed file at PR head against the protected base before voting. Durable: `trusted-gate-runner.py` + signed attestation, executing outside the application's own CI context entirely | **Transitionally mitigated, live**; durable fix built, not deployed. See [`GATE_CONTRACT.md`](GATE_CONTRACT.md) |
| T4 | A scanner's own built-in secret/pattern detector flags another step's prior report file, sitting in the same shared workspace, as a real finding — a false positive that would permanently block every PR regardless of actual code | [SADR-0018](adr/0018-unified-findings-gate-live-and-self-contamination.md), live: Semgrep's `p/secrets` flagged `gitleaks-report.json` as a leaked Facebook OAuth token | Explicit `--exclude` of every platform-generated report filename from Semgrep's and Trivy's own scan targets | **Closed** for the scanners currently wired in; the same class of risk applies to any new scanner with its own secret/pattern detector — see [`POLICY.md`](POLICY.md)'s "Adding a new scanner" |
| T5 | A lost or undelivered webhook leaves a PR stuck with no terminal status, and fail-closed design (D4) makes that indistinguishable from "the gate is working as intended" until someone notices | [SADR-0015](adr/0015-reconciliation-loop.md), live against a deterministically stuck PR (opened before Woodpecker activation) | `reconciliation-loop.py` polls for open PRs with no terminal status and re-triggers via a real, empty git commit — deliberately not a synthesized Woodpecker/Gitea event, since both available synthesis mechanisms were checked against source and found to produce a push-shaped event that could satisfy branch protection's status glob without a genuine `pull_request` evaluation, reopening T1's class of bug | **Closed**, live-tested, includes a runaway-safety cap (max 3 nudges) |

## Open — tracked, not swept under anything

| # | Threat | Why it's still open | Where it's tracked |
|---|---|---|---|
| T6 | The Sprint 01 transitional control (T3's mitigation) proves gate-managed *files* weren't changed; it does not prove the gate ran in an isolated, tamper-proof environment. The scanning steps still execute inside the same Woodpecker agent that runs the PR's own untrusted build | No isolated runner host exists in this environment (one machine total) to deploy `trusted-gate-runner.py` against | [`GATE_CONTRACT.md`](GATE_CONTRACT.md), `docs/TODO.md` Milestone 3 items |
| T7 | `GITEA_ADMIN_TOKEN` holds a standing push-whitelist bypass of PR review on every onboarded repo — a design tradeoff stated plainly in `onboard-repo.sh`'s own comments, accepted for Sprint 01's scope | The platform-admin's own automation needs to push updated pipeline templates without being blocked by the same branch protection it sets up; no narrower-scoped identity exists yet | `docs/TODO.md`'s "Fast-gate/pipeline debt" section; D2's "audit PATs for broad `write:repository` scope" applies to this token specifically |
| T8 | The regression suite's Tier 2 (real nested Gitea+Woodpecker, genuinely isolated second agent) is designed but not built — "isolation" tested against a single shared host would not actually prove isolation | This environment has only one machine | [SADR-0016](adr/0016-self-verify-ci-wiring.md); same honest gap noted for the Ansible playbooks (SADR-0010) |
| T9 | `bot-approver.py` and `reconciliation-loop.py` are both single-repo, manually-started processes, not standing services with restart/health monitoring | Not yet wired into `compose/minimal` as services | `docs/TODO.md`, [`ARCHITECTURE.md`](ARCHITECTURE.md)'s current-state table |
| T10 | No baseline/differential gating exists — a repo with pre-existing findings cannot be onboarded without blocking every PR from day one, which is itself a rollout risk (teams route around a gate that punishes history) | Depends on exceptions-repo infrastructure, Milestone 4 | [`EXCEPTIONS.md`](EXCEPTIONS.md) |
| T11 | No append-only evidence store outside Gitea itself — a compromised credential with Gitea write access could in principle alter the history meant to prove the gate worked | Not built | [`COMPLIANCE.md`](COMPLIANCE.md)'s evidence-source table |
| T12 | Container images in `compose/minimal` are pinned by exact tag, not digest — a tag is mutable at the registry even when the version string looks fixed | `gate-bundle/run-container.sh` enforces digest-only for the trusted-runner path; `compose/minimal` does not yet apply the same rule | [`SELF_DEFENCE.md`](SELF_DEFENCE.md) |
| T13 | Gitea itself is a high-value target and a high-CVE-volume dependency (84 advisories in a four-month window, per direct GHSA research) — several bear directly on this platform's own trust decisions (the retarget bypass behind T2, WebAuthn/TOTP bypass on the OAuth2 sign-in path relevant to any future SSO integration) | Patch cadence is fast; no automated advisory-monitoring job exists, only a documented manual query | [`SELF_DEFENCE.md`](SELF_DEFENCE.md), `docs/PINNED_VERSIONS.md`'s CVE-posture section |

## Threats considered and deliberately not built around (yet)

- **Fork-based secret exfiltration / fork-pipeline secret withholding.** Not applicable today — forks
  are disabled org-wide per D2, which removes this entire class rather than mitigating it. Revisit if
  forks are ever re-enabled; D2 already specifies fork-triggered pipelines would need their own
  unprivileged agent with no secrets.
- **CI agent Docker-socket escape.** DESIGN.md's *Deployment profiles* names this directly: the agent
  mounts the Docker socket, so a container escape from any build step compromises the CI host and
  every secret on it. `gate-bundle/run-container.sh`'s hardening (`--network none --read-only
  --cap-drop ALL --security-opt no-new-privileges`, no Docker socket mounted into the *gate* container
  itself) addresses the trusted-runner path specifically once deployed (T6). It does not address the
  ordinary Woodpecker agent that runs every onboarded repo's own untrusted build steps today — running
  rootless or on a separate agent VM remains unimplemented.
- **DefectDojo/exceptions-repo compromise affecting the merge decision.** Structurally prevented by D5
  rather than mitigated after the fact: the merge decision never depends on either service being
  reachable, by design, so this threat class doesn't exist yet in the current architecture — it will
  need re-evaluating once the exceptions repo is built, since a compromised exceptions repo *could*
  then forge an accepted-risk record.

## How to use this document

Before treating anything here as settled: check `docs/TODO.md` and [`ARCHITECTURE.md`](ARCHITECTURE.md)'s
current-state table for whether the underlying mechanism is still accurate — this file, like
`docs/PINNED_VERSIONS.md`, decays and should be re-checked at each milestone boundary. A row marked
"Closed" means live-tested and confirmed working as of the cited SADR, not "can never regress" — the
regression suite (`tests/regression/`) exists specifically to catch a closed threat reopening silently.
