# What next

Two independent external critiques of this codebase, received 2026-08-28 and verified against
actual source before being recorded here — not transcribed on trust. They converge closely (same
assessment table, same critical-gaps ordering, same recommended sequence), which is itself useful
signal; one of the two (`latest_critique.md`, at the repo root) additionally breaks the top-priority
work into concrete tranches with exit criteria, folded in below. Every checkable claim in either
held up: test counts (20/17/8/5/5/4/3/5/1, all confirmed by re-running `tests/unit/run-unit-tests.sh`
live), `sidecar/`, `templates/`, and `kubernetes/` all genuinely empty, no
`terraform/<cloud-provider>/`, no `git remote` on the local checkout. This document is the
synthesis: both critiques' findings, cross-referenced against the SADRs and `docs/TODO.md` items
that already track each gap (so this file doesn't become a second, drifting source of truth), plus
one correction where a literal claim needs a nuance.

**Read this alongside [`docs/TODO.md`](TODO.md)**, not instead of it — TODO.md is the
granular, currently-open item list; this file is the higher-altitude "why does the order matter"
view and the priority call.

## The one-sentence verdict

Strong threat model, unusually thorough bypass testing, honest about its own limits — but still an
**enforcement pilot**, not a production SSDLC service. The most important gap: the durable trust
boundary (an independently controlled runner producing a signed gate decision) exists in code and
is unit-tested, but has never run. Everything else is secondary to that one fact.

## Overall assessment

| Area | Assessment | Principal gap |
|---|---|---|
| Security design | Strong | Target architecture is considerably ahead of deployed reality |
| PR scanning | Good pilot | Fast gate covers secrets, SAST, and dependencies only |
| Merge enforcement | Moderate | Transitional protected-file/tree comparison remains the active mechanism |
| Supply-chain integrity | Early | No complete SBOM, signing, provenance, or registry promotion chain |
| Exception management | Missing | No usable two-party risk-acceptance path exists at all |
| Developer experience | Early | Native Gitea/Woodpecker screens and shell scripts; no unified portal |
| Evidence and compliance | Weak | No persistent evidence store, metrics, or export mechanism |
| Platform operations | Weak | No automated backup, restore drill, monitoring, or production edge |
| Test quality | Strong locally | Isolated live-integration tier (Tier 2) is not operational |
| Rollout readiness | Pilot only | Single-repository/manual components and unresolved rules licensing |

## What's done well (kept brief — the SADRs are the actual record)

The project's real strength is that it evaluates *bypasses of the merge decision itself*, not just
"does a scanner run": commit-status forgery ([SADR-0001](adr/0001-commit-status-forgery.md)),
PR-retargeting ([SADR-0003](adr/0003-pr-retargeting-approval-bypass.md)), stale human and stale bot
approvals, self-approval, two-humans-substituting-for-the-bot, a PR modifying its own pipeline or
vendored policy ([SADR-0017](adr/0017-trusted-gate-contract.md),
[SADR-0019](adr/0019-gate-contract-bypass-regression.md),
[SADR-0020](adr/0020-vendored-semgrep-rules.md)), missing/malformed/truncated scanner evidence,
lost-webhook reconciliation ([SADR-0015](adr/0015-reconciliation-loop.md)), private-repo onboarding,
and secret detection against a real archive-shaped workspace
([SADR-0023](adr/0023-gitleaks-no-git-workspace.md)). That list is materially more mature than "runs
Semgrep and Trivy." The severity policy is centralized in one reviewable Rego file
([SADR-0018](adr/0018-unified-findings-gate-live-and-self-contamination.md)), fails closed on missing
evidence, and the attestation design now covers both a contract digest and a policy digest
([SADR-0022](adr/0022-attestation-policy-digest.md)). And the project is honest in its own record
about what's locally-verified vs. live-tested vs. mocked vs. transitional vs. undeployed — that
honesty is worth deliberately preserving as new work lands, not just a nice-to-have.

## Critical gaps

### 1. The durable gate trust boundary is not deployed

`scripts/trusted-gate-runner.py` and `gate-bundle/` exist, are unit-tested, and (per SADR-0022/0023)
have had two real production bugs found and fixed against the actual built image. But the isolated
runner has never run. The active protection is still `GATE_CONTRACT_ENFORCE`'s transitional
file/tree comparison — real, but not equivalent to an independent gate: a new attack surface outside
the managed-paths/managed-tree-prefixes list would escape it entirely, the trusted scan doesn't run
on a separately administered host, `GATE_ATTESTATION_REQUIRED` stays `0`, and the signing mechanism
is an acknowledged HMAC pilot bridge. Before any real team depends on this: deploy the runner on a
separate host, release the bundle independently of the application checkout, replace HMAC with a
real signing identity, pin the expected signer/contract-digest/policy-digest/bundle-digest, require a
current-head signed decision, and prove success/blocking/missing-attestation/policy-tampering
scenarios live — retaining both passing and failing decisions as evidence. **Blocked purely on
infrastructure** (this environment has one machine) — see [`GATE_CONTRACT.md`](GATE_CONTRACT.md).

### 2. The CI runner has host-root capability

The Woodpecker agent mounts `/var/run/docker.sock` (`compose/minimal/docker-compose.yml`). A PR that
controls its own pipeline container has a real path toward controlling the host and every secret on
it. `GATE_CONTRACT_ENFORCE` closes one route (a PR can't silently swap the evaluator) but does not
make socket exposure safe. Real fixes: dedicated disposable runner VMs, rootless build execution, a
remote BuildKit service with restricted entitlements, ephemeral workers recreated per build, separate
trusted/untrusted runner pools, no platform credentials inside ordinary build containers, and network
restrictions stopping a build step from reaching control-plane services. **The trusted runner (#1)
must never share this privilege boundary with PR-controlled builds** — that's the whole point of
building it separately.

### 3. No exception path exists

**Update: the baseline half of this is now built (SADR-0024/0025).** `policy-eval/evaluate-findings.py
--baseline` splits findings into new (still blocks Critical/High unconditionally) and pre-existing
debt (reported, never blocking), using stable fingerprints and a default-branch baseline generated at
onboarding (`scripts/generate-baseline.py`) that can only shrink. This is what used to block onboarding
any repo with pre-existing findings — it no longer does.

What remains genuinely absent: false-positive suppression, two-party risk acceptance, expiry, and
enforcement/audit records for a genuinely NEW finding someone wants to accept rather than fix.
[`EXCEPTIONS.md`](EXCEPTIONS.md) documents the intended design for that remaining half in full; none of
it is built. A gate with no usable exception path is exactly the shape that gets quietly disabled by an
admin under real deadline pressure — that's the actual production risk, not just a UX gap. Needed:
separate suppression records from risk-acceptance records, two-party acceptance with named
Security-Officer/Eng-Lead roles, hard expiry with automatic re-blocking, and append-only audit evidence.

### 4. Secret detection happens too late

`compose/forgery-test/pre-receive-gitleaks.sh` exists and is live-tested (SADR-0002) but is not
installed by `onboard-repo.sh` or Ansible on any real onboarded repo — tracked in `docs/TODO.md`'s
Milestone-3 section. CI-time detection means the credential has already reached Gitea's object store,
webhook payloads, build logs, and every clone before anyone finds out. The pre-receive hook needs to
ship as an operationally safe chained hook (preserve Gitea's own generated hook, scan only incoming
objects, read suppressions only from the protected default branch, measured latency against a
realistic repo size) — and blocking the push doesn't eliminate the need to actually rotate a secret
that already leaked once.

### 5. There is no evidence system

Nothing persists a durable record linking repository → PR → base/head SHA → source-tree digest →
bundle digest → policy digest → scanner versions → raw and normalized findings → decision →
exception state → approval → merge → artifact → deployment. Woodpecker's own logs are the only
record today, and they are not retention-controlled, access-controlled, or exportable. Without this,
the platform can block a merge but cannot *prove*, later, why it allowed or rejected a release —
which is the entire point of the ISO-evidence goal `COMPLIANCE.md` names.

## "Frontend" / developer-experience critique

No custom frontend exists (D10's portal is Milestone 8, correctly deferred). Developers interact
through Gitea PRs, Woodpecker's own screens, one reviewdog comment (Semgrep only —
`pipelines/fast.woodpecker.yml`'s `inline-comments` step never feeds it Trivy or Gitleaks output),
shell scripts, and docs. Fine for a single pilot repo; weak past that. Missing: a consolidated view
of what failed, whether it's new or inherited, remediation guidance, whether an exception is
possible and from whom, current exception expiry, and current gate status for the head commit. The
proposed thin operator control plane (repo onboarding status, gate health, stale PRs, attestation
failures, scanner/rule freshness, exception queue, runner health, evidence export) should stay
explicitly *not* a new system of record — Gitea, the (not-yet-built) exceptions repo, and the
evidence store keep authority.

**`templates/` is empty** despite `DESIGN.md`'s repository structure naming it as the paved-road
scaffolding surface (`adr/`, `threat-model.md`, `.pre-commit-config`). A real paved road needs
language-specific pipeline includes, secure Dockerfile examples, dependency-update config, SBOM/
image-signing integration, a suppression schema with expiry, CODEOWNERS guidance, and a standard
release workflow — onboarding should let a repo select its stack and install only the relevant
controls, not receive everything.

## Backend / enforcement critique

- **`bot-approver.py` is single-repo, manual, plain-env-vars** — confirmed zero references to it in
  `compose/minimal/docker-compose.yml`. Needs multi-repo discovery, webhook-driven evaluation, a
  durable work queue, idempotent processing, retry/dead-letter handling, health/readiness endpoints,
  and audit events per decision, before it's infrastructure rather than a script someone remembers to
  run. `scripts/reconciliation-loop.py` is the natural component to fold into the same supervised
  service, per its own SADR-0015 note.
- **`GITEA_ADMIN_TOKEN` is too broad** — `onboard-repo.sh`'s own comments already name this as a
  standing PR-review bypass on every onboarded repo (`docs/TODO.md`). Needs a narrowly scoped
  onboarding identity, separate identities per function (comments / scanning / approval /
  administration), and treatment of the gate-bot token specifically as a crown-jewel credential —
  DESIGN.md already uses that language; it should be operationalized, not just stated.
- **Policy evaluation now has introduced-vs-inherited state (SADR-0024/0025), still nothing beyond
  that** — no repository risk tier, no reachability/exploitability weighting, no exception state read
  at evaluation time (that's item 3 above, still open). Don't over-engineer this early, but a versioned
  policy input document now avoids rewriting the normalise adapters later when this grows.
- **Scanner coverage is real but narrow**: Gitleaks + Semgrep OSS + Trivy filesystem SCA only.
  Missing from the *active* fast/deep gate: IaC scanning, container-image scanning tied to the built
  digest, SBOM generation, license compliance, package-reputation checks, structured DAST evidence,
  Kubernetes admission policy, and continuous re-evaluation of already-shipped SBOMs. Semgrep OSS's
  lack of cross-file taint analysis (a Pro-only feature) should be stated in any security reporting
  this platform produces, not left implicit.
- **No complete artifact promotion chain** — Syft/Cosign/provenance are named in `DESIGN.md` but not
  implemented. A real release gate builds once, records source revision and build environment,
  generates an SBOM, scans the actual built image, produces provenance, signs image and attestations,
  pushes to a controlled registry, and promotes *the same digest* between environments — rebuilding
  separately per environment defeats provenance entirely.
- **DAST is experimental** — `pipelines/dast-scheduled.woodpecker.yml` exists and is live-tested
  (SADR-0006/0007), but its structured report output and cron registration are both open
  (`docs/TODO.md`). Get ZAP reliable and operationally owned (staging target per repo, safe scan
  profile, dedup, ownership/notification, a stated block-vs-warn rule) before adding Nuclei for
  broader coverage.

## Supply-chain and governance gaps

- **Vendored rules licensing is genuinely unresolved** — `policy/vendored-rules/README.md` already
  states this plainly (LGPL-2.1 + Commons Clause, internal-use reading not reviewed by counsel). This
  is a real blocker on broad rollout or making the repo public, not a formality — a platform whose own
  compliance tooling carries an unreviewed licensing question is a bad look in exactly the kind of
  audit this platform exists to support. Needs: recorded upstream source/revision (already done —
  `f1d2b562b414783763fd02a6ed2736eaed622efa`), a written internal-use approval, a defined distribution
  boundary, and a reviewed update process.
- **Trivy DB ownership is half-done** — the persistent cache and refresh pipeline exist (SADR-0021)
  but the cron isn't registered and there's no freshness SLA, alerting, or recorded linkage between
  "which DB version informed this decision" and the gate's own evidence trail (once #5 exists).
- **The platform has never dogfooded itself through its own complete intended gate** — SADR-0010
  already found this partially hollow even where attempted (Checkov has no policies for the `docker`
  Terraform provider's resource types). Real dogfooding needs its own pipeline protected from PR
  modification, its IaC scanned meaningfully, its images scanned and signed, its own SBOM retained,
  and its own gate bundle independently released — a genuine credibility gap until then.
- **Identity source remains unresolved for the roles Tranche 2's two-party exceptions need** —
  `DESIGN.md`'s own open question 4 (Gitea local accounts vs. LDAP/OIDC) is what decides how
  "Security Officer" and "accountable Engineering Lead" get established as real, verifiable roles
  rather than "whoever has the right Gitea team membership today." This blocks Tranche 2 specifically,
  not just a general nice-to-have — a two-party approval process is only as strong as the identity
  behind each party.

## Infrastructure and operations

The `minimal` profile is correctly scoped for a lab, not production. Confirmed gaps: no reverse
proxy/TLS profile, plaintext `.env` secrets (no sops/age deployed despite being named in
`DESIGN.md`), no backup script, no restore-drill script, no evidence-export script, no monitoring
stack, no log aggregation, Ansible never exercised end to end on a real POSIX control node (SADR-0010
— native Windows can't run it, needs WSL2/Linux CI/macOS), no cloud Terraform, empty `kubernetes/`,
and no recovery-time/recovery-point objectives anywhere. Worth stating plainly: **Gitea, Woodpecker,
and Postgres are part of the enforcement control, not just developer infrastructure** — losing them
doesn't just inconvenience developers, it can erase audit history or halt all delivery. This raises
the backup/restore-drill gap from "nice to have" to "the evidence story doesn't exist without it."

## Repository hygiene — confirmed, with one correction

**Confirmed:** `git rev-parse --show-toplevel` from inside this directory resolves to
`C:\applications`, not `C:\applications\SSDLC-Platform-Lab`. Every sibling client project
(`HR_system`, `edge_agent`, `ACS`, and a dozen others) sits in the same repo as an untracked
directory. This is a real, live risk: accidental staging of unrelated projects, confusing relative
paths in automation, and a future onboarding/release script committing content it has no business
touching. This needs a deliberate repository migration (a clean extraction of `SSDLC-Platform-Lab`'s
own history into its own repo root) — not an automated rewrite done in passing.

**Correction to "the checkout has no configured Git remote, so local history is not currently backed
by an upstream repository":** true and unchanged for `C:\applications` itself — `git remote -v`
there is empty. But this project's actual work **is** backed by a real, currently-maintained GitHub
upstream: `github.com/WillemKlopper87/ssdlc-platform-lab`, reached via a deliberate workaround for
exactly the repo-root problem above — each commit is exported (`git archive`) from the shared
`C:\applications` checkout into a standalone tree and pushed from there, not via a direct `git push`
from this checkout. That workaround is real infrastructure debt in its own right (a manual multi-step
process, not `git push`), but "not backed by an upstream" would overstate the actual exposure. Fixing
repo hygiene (the item above) would let this become a normal, direct `git push` again.

## Test critique

The unit tier is genuinely solid and reproducible: 20 + 17 + 8 + 5 + 5 + 4 + 3 + 5 + 1 = 68 assertions
across 9 files plus the Rego suite, all independently re-run and confirmed passing as of this
document. The gap is evidence *realism*, not evidence *quantity*: Tier 2 (live integration) isn't
running automatically, the trusted runner isn't live, Ansible hasn't touched a real POSIX controller,
backups/restores are untested, real repo size and concurrency are unmeasured, multi-repo operation is
untested, and there's no standing canary repo continuously proving both the allow and deny paths.
A useful canary suite would continuously prove: clean PR passes; secret push rejected before storage;
new High blocks; Medium warns; altered pipeline can't self-approve; stale approval fails; valid
exception passes until expiry, then re-blocks; missing scanner evidence fails closed; signed artifact
admitted, unsigned/mismatched rejected. None of that exists yet — it's a natural home for whatever
replaces `pipelines/self-verify.woodpecker.yml`'s current scope once #1 and #3 above exist.

## Recommended order

1. **Move the project into a dedicated Git repository root and configure a direct upstream remote** —
   closes the repo-hygiene gap and the manual-export workaround it currently requires. Needs explicit
   sign-off before touching shared history; not something to do unilaterally given other sessions
   share this checkout.
2. **Deploy the isolated trusted runner and replace HMAC with an independent signing identity.**
3. **Require signed, head-bound gate attestations for one pilot repository.**
4. ~~**Implement baseline/differential gating**~~ — done, live-verified, SADR-0024. **The two-party
   exception workflow** remains open — it depends on Milestone 4's exceptions-repo infrastructure.
5. **Install pre-receive secret detection** with measured latency and a rotation procedure.
6. **Turn the bot/reconciliation logic into a supervised multi-repository service.**
7. **Add durable evidence storage and per-release evidence export.**
8. **Complete SBOM, image scanning, provenance, signing, and registry promotion.**
9. **Register Trivy/DAST schedules and operational freshness alerts.**
10. **Add scoped identities, sops/Vault-backed secrets, and Entra/OIDC group mapping.**
11. **Add monitoring, backups, restore drills, and a production edge/TLS profile.**
12. **Build the thin developer/operator portal.**
13. **Expand deep scanning and deployment/runtime controls** — only after the core trust path (1–4)
    is proven; more scanners without a trustworthy merge decision is breadth without the thing that
    actually matters.
14. **Complete licensing approval and formal `FRAMEWORK-ADDENDUM-2026.md` approval** before broad
    rollout — a hard gate on *rollout scope*, independent of technical readiness on the items above.

**The next tranche is 2–4.** Items 2 and 3 are blocked on infrastructure this environment doesn't
have (a second, isolated host). Item 4's baseline/differential-gating half is now done (SADR-0024,
live-verified); its two-party exception workflow half is not, and stays blocked on Milestone 4's
exceptions-repo infrastructure like items 2/3. Item 1's repo migration remains directly actionable
here but needs explicit user authorization given its blast radius.

## Tranche breakdown for items 2–4 (once a second host exists)

Concrete sub-steps, not just a restated goal — this is what "deploy the trusted runner" actually
decomposes into:

**Tranche 1 — isolated trusted pilot.** Provision a separate trusted-runner host/security boundary.
Install the independently built bundle by immutable digest (`gate-bundle/run-container.sh` already
refuses anything but `@sha256:...` — confirmed, this part is ready). Replace HMAC, or explicitly and
narrowly bound the temporary bridge to the pilot only if replacing it isn't feasible yet. Configure
the expected contract and policy digests (`scripts/print-gate-contract-digest.py`,
`scripts/print-policy-digest.py` — both exist and are live-tested per SADR-0022). Enable
`GATE_ATTESTATION_REQUIRED=1` for **one** repository only. Live-test clean, failing, tampered,
missing, and stale-head decisions. Retain evidence and document the rollback path.
**Exit criterion, stated precisely: a PR-controlled build must not be able to manufacture the
merge-authorising evidence** — not "the happy path works," but "the attack doesn't."

**Tranche 2 — usable enforcement.** Baseline/differential gating is done (SADR-0024) — found and
fixed two real bugs live along the way, including that Trivy's own `fingerprint` field (which
SADR-0018's docstring assumed was stable) actually isn't across scans that don't touch the vulnerable
manifest at all; `normalise/trivy_adapter.py` no longer trusts it. Remaining for this tranche:
materialize false-positive suppressions and two-party exceptions per
`EXCEPTIONS.md`'s design. Enforce expiry and reactivation. Actionable PR feedback, and start
measuring exception wait time (`DESIGN.md`'s own "the gate's survival metric"). Keep peer review
independent of security acceptance — an exception unblocks the security gate, never the review gate.

**Tranche 3 — secrets before storage.** Package `compose/forgery-test/pre-receive-gitleaks.sh` as the
chained Ansible role `docs/TODO.md` already names. Define protected-branch suppression semantics
(read only from the default branch, never the incoming ref — SADR-0002 already proved this
specific property live). Benchmark against realistic repo sizes, not the ~100-byte fixture SADR-0002
used. Test push rejection and recovery. Link detections to rotation/incident procedure without
exposing the actual secret value anywhere in that link.

**Tranche 4 — service and evidence.** Multi-repo `bot-approver.py`/`reconciliation-loop.py` as one
supervised service, with a durable queue, idempotency, and health endpoints. Persist signed
decision/evidence records. Export per-PR/per-release audit packs.

**Tranche 5 — artifact trust.** Build once, generate SBOM, scan the actual built image, produce
provenance, sign, publish to a controlled registry, verify at deployment. This is explicitly *after*
1–4, not parallel to them — signing an artifact from a merge decision that itself isn't trustworthy
signs the wrong thing.

## Verification gates — apply to every tranche above

- Run the full unit tier (`tests/unit/run-unit-tests.sh`) — syntax, policy, normaliser, evaluator,
  bot, attestation, bundle, Rego.
- Run live bypass regressions against real (not mocked) Gitea/Woodpecker infrastructure.
- **Prove failure paths, not only clean merges** — this project's own established discipline
  (SADR-0001 onward) already does this; keep doing it as scope grows.
- Record the exact repo, PR, head SHA, and bundle/policy/contract digests behind every decision
  claimed as evidence.
- Explicitly distinguish local, mocked, live-pilot, and production evidence in whatever record gets
  produced — do not let a mocked-test pass read as a live proof, in either direction.
- Validate that secrets never enter an ordinary (PR-controlled) build container at any point.
- Test idempotent onboarding and reconciliation across multiple repositories, not just one.
- Run backup/restore and upgrade/rollback rehearsals before anything here reaches production.
- **Update the relevant SADR, `ARCHITECTURE.md`, `docs/TODO.md`, and `OPERATIONS.md` with every
  tranche** — this document decays like `PINNED_VERSIONS.md` does if the underlying docs drift out
  from under it; re-check it against current state at each milestone boundary, not trust it
  indefinitely.

## Recommended immediate action

Deploy the isolated trusted runner (Tranche 1), then implement the exception/baseline path
(Tranche 2). Adding more scanners before those two controls are operational creates breadth without
resolving the one question that actually matters: whether the merge decision itself is independently
trustworthy and usable by a real team. Everything in this document is secondary to that.
