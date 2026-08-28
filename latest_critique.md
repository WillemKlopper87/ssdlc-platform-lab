# SSDLC platform codebase evaluation and critique

**Reviewed and regenerated:** 2026-08-28  
**Scope:** `C:\applications\SSDLC-Platform-Lab`, assessed as an SSDLC enforcement and evidence platform.

## Executive assessment

The project has a strong threat model and unusually thoughtful bypass testing, but it remains an enforcement pilot.
The most important gap is that the durable trust boundary—an independently controlled runner producing a signed,
head-bound gate decision—exists in code but is not deployed.

| Area | Assessment | Main concern |
|---|---|---|
| Security design | Strong | Target architecture is ahead of deployed reality |
| PR scanning | Good pilot | Fast gate covers secrets, SAST and dependencies only |
| Merge enforcement | Moderate | Transitional protected-file comparison remains active |
| Supply-chain integrity | Early | No complete SBOM/signing/provenance/promotion chain |
| Exception management | Missing | No usable two-party risk-acceptance path |
| Developer experience | Early | Scripts and native Gitea/Woodpecker screens only |
| Evidence/compliance | Weak | No persistent evidence store, metrics or export |
| Operations | Weak | No automated recovery proof, monitoring or production edge |
| Test quality | Strong locally | Isolated live integration tier is not operational |

## Current validation evidence

The unit tier passed during review:

- shell and Python syntax checks;
- Woodpecker interpolation lint;
- 20 approval-policy assertions;
- 17 normalisation assertions;
- 8 findings-evaluator assertions;
- 5 bot-approver tests;
- 5 attestation tests;
- 4 trusted-runner tests;
- 3 gate-bundle tests;
- 5 Rego tests;
- private-onboarding source validation.

These results do not prove the trusted runner is deployed or production safe.

## Strengths to preserve

- Commit-status forgery, retargeting and stale-approval threats are explicitly tested.
- Current-head bot approval and a distinct human approval are required.
- PR-controlled pipeline, policy and vendored-rule changes are checked against the protected base.
- Missing, malformed, empty and truncated evidence fails closed.
- Gitleaks, Semgrep and Trivy findings are normalised into one Rego policy.
- Scanner and policy versions/digests are treated as security inputs.
- Vendored rules avoid uncontrolled upstream policy drift.
- Private repository onboarding and lost-webhook reconciliation have live evidence.
- The project clearly distinguishes local, mocked, live and undeployed claims.

## Critical gaps

### 1. Trusted gate boundary is not deployed

The trusted bundle and runner exist, but enforcement still relies on transitional protected-file/tree comparison. The
runner is not isolated, signed attestation is not required and HMAC remains a pilot bridge.

Before real rollout:

1. Deploy a separately administered runner.
2. Release the gate bundle independently of application repositories.
3. Replace HMAC with an independently controlled signing identity.
4. Pin signer, contract, policy and bundle digests.
5. Require a current-head signed decision.
6. Live-prove success, policy tampering, missing attestation and failing decisions.
7. Retain all decisions as evidence.

### 2. Docker socket makes the ordinary runner host-root equivalent

The Woodpecker agent mounts `/var/run/docker.sock`. PR-controlled execution can compromise the host if the pipeline
trust boundary is bypassed. Production should use disposable/dedicated runner VMs, rootless execution or restricted
remote builders, with separate trusted and untrusted pools and no platform credentials in build containers.

### 3. Exception and baseline workflow is absent

Every Critical/High finding blocks regardless of whether it predates onboarding. There is no default-branch baseline,
false-positive suppression, two-person risk acceptance, expiry or reactivation.

Implement before broad enforcement:

- stable finding fingerprints;
- baseline generated at onboarding and allowed only to shrink;
- PR gating on introduced/reintroduced findings;
- separate false-positive suppressions with owner and expiry;
- Security Officer plus accountable Engineering Lead approval;
- ticket, justification and maximum expiry;
- automatic re-blocking and quarterly review;
- append-only evidence and DefectDojo/register integration.

### 4. Secret detection occurs after Git receives the object

The pre-receive Gitleaks experiment is not installed. CI detection is too late: secrets may already exist in Gitea,
webhooks, logs and clones. Package a chained pre-receive hook that scans incoming objects, reads suppressions only from
the protected default branch, has measured latency and triggers credential-rotation procedures.

### 5. Evidence retention is missing

A production record must link repository, PR, base/head SHA, source digest, gate bundle, policy/rules, scanner versions,
raw/normalised findings, exception, approvals, merge, artefact and deployment. Evidence should be immutable or
append-only, access-controlled, retention-managed, signed/hash-chained and exportable per release and audit period.

## Developer and operator experience

There is no custom frontend. Users rely on Gitea, Woodpecker, reviewdog, scripts and documentation. That is sufficient
for a pilot, but developers need a consolidated explanation of failures, remediation, new-versus-baseline state,
exception eligibility, approval status and rescan state.

The proposed portal should remain a thin aggregation layer. It should expose:

- repository onboarding and posture;
- gate and attestation health;
- scanner/rules/DB freshness;
- stale PR and exception SLA queues;
- runner capacity and reconciliation failures;
- per-release evidence export.

It must not become a new security system of record.

`templates/` also needs real paved-road content: supported-language pipelines, secure Dockerfiles, dependency updates,
coverage defaults, SBOM/signing, suppression schema, CODEOWNERS guidance and standard release workflows.

## Enforcement/backend critique

### Bot operation is single-repository and manual

Convert the bot and reconciliation logic into a supervised multi-repository service with webhook processing, durable
queueing, idempotency, retries/dead letters, rate-limit handling, per-repository policy, health endpoints and auditable
decisions.

### Administrative identity is overprivileged

Onboarding depends on a broad Gitea admin token. Split identities for onboarding, comments, scanning, approvals and
administration. Scope each to repositories/teams, rotate credentials and never place the gate-bot credential in a build
container.

### Policy lacks context

The current policy is absolute severity. Introduce a versioned decision input that can later include baseline state,
repository risk tier, asset exposure, fix availability, exploitability, exception state and PR-versus-release context.
Keep the first production policy understandable.

### Scanner coverage is incomplete

The fast gate has Gitleaks, Semgrep OSS and Trivy filesystem SCA. Important missing layers include IaC enforcement,
image scanning tied to the built digest, SBOM, licence compliance, structured DAST, API security testing, Kubernetes
admission and runtime detection. Semgrep OSS also lacks cross-file taint analysis; reporting should state this limit.

### Artefact promotion chain is missing

A production pipeline should build once, generate an SBOM, scan the actual image, issue provenance, sign image and
attestations, push to a controlled registry, promote the same digest and verify policy at deployment.

### DAST remains experimental

ZAP scheduling and structured output remain unresolved. Add per-application staging targets, authentication, safe scan
profiles, destructive-endpoint exclusions, JSON/SARIF/HTML evidence, deduplication, ownership and a clear rule for how
scheduled findings affect release eligibility.

## Governance and supply-chain gaps

- OpenGrep-derived vendored rules need written licensing approval and provenance/update records.
- The Trivy DB refresh pipeline is not registered and has no freshness SLA/alert.
- The platform does not yet release itself through its own complete gate.
- The framework addendum has not completed formal approval.
- Gitea local identity versus LDAP/OIDC remains unresolved for security approver roles.

## Infrastructure and operations gaps

- No reverse proxy/TLS deployment profile.
- Platform secrets remain plaintext in `.env` files.
- SOPS/Vault workflow is designed but not deployed.
- No automated backup, restore drill or evidence export scripts.
- No metrics, dashboards or log aggregation.
- Ansible has not been exercised end to end.
- No cloud Terraform target.
- `kubernetes/` is empty.
- No HA, recovery objectives or upgrade/rollback automation.
- No production capacity or multi-repository performance evidence.

## Repository hygiene issue

The Git top level resolves to `C:\applications`, not the project directory. Sibling projects therefore appear as
untracked content. This risks accidental staging, wrong scan scope and inconsistent automation. The SSDLC platform
should have its own repository root and configured upstream. Migrate deliberately; do not rewrite history silently.

## Testing gaps

- Tier 2 live integration is not automated on an isolated host.
- Trusted runner and attestation are not live.
- Ansible has not run on a real POSIX controller.
- Backup/restore is untested.
- Real repository size, throughput and multi-repository behaviour are unmeasured.
- Registry/rate-limit/scanner-outage failure tests are limited.
- No standing canary repository continuously proves allow and deny paths.

A canary should prove clean pass, pre-receive secret rejection, High block, Medium warning, tamper rejection, stale
approval rejection, valid/expired exception behaviour, missing-evidence failure and signed/unsigned artefact admission.

## Recommendation order

1. Move to a dedicated repository root and configure an upstream.
2. Deploy the isolated trusted runner and independent signing identity.
3. Require signed head-bound attestations for one pilot repository.
4. Implement baseline/differential gating and two-party exceptions.
5. Install pre-receive secret detection and rotation procedures.
6. Convert bot/reconciliation into a supervised multi-repository service.
7. Add durable evidence storage and release evidence export.
8. Complete SBOM, image scan, provenance, signing and registry promotion.
9. Register Trivy/DAST schedules and freshness alerts.
10. Add scoped identities, encrypted secrets and OIDC group mapping.
11. Add monitoring, backups, restore drills and production TLS/edge.
12. Build the thin developer/operator portal.
13. Expand deep scanning and runtime controls after the trust path is proven.
14. Complete licensing and framework approvals before broad rollout.

## Next-agent plan

### Tranche 1: isolated trusted pilot

- Provision a separate trusted-runner host/security boundary.
- Install the independently built bundle by immutable digest.
- Replace HMAC or clearly bound the temporary bridge to the pilot.
- Configure expected contract and policy digests.
- Enable required attestation for one repository only.
- Live-test clean, failing, tampered, missing and stale-head decisions.
- Retain evidence and document rollback.

Exit only when a PR-controlled build cannot manufacture the merge-authorising evidence.

### Tranche 2: usable enforcement

- Implement baseline/fingerprints.
- Materialise false-positive suppressions and two-party exceptions.
- Enforce expiry and reactivation.
- Provide actionable PR feedback and measure exception wait time.
- Keep peer review independent of security acceptance.

### Tranche 3: secrets before storage

- Package the chained pre-receive hook.
- Define protected-branch suppression semantics.
- Benchmark realistic repositories.
- Test push rejection and operational recovery.
- Link detections to rotation/incident procedures without exposing values.

### Tranche 4: service and evidence

- Build multi-repository bot/reconciliation service.
- Add durable queue, idempotency and health.
- Persist signed decision/evidence records.
- Export per-PR/per-release audit packs.

### Tranche 5: artefact trust

- Build once, generate SBOM, scan image, produce provenance, sign, publish and verify at deployment.
- Add a controlled registry and immutable promotion.

## Verification gates

- Run unit syntax, policy, normaliser, evaluator, bot, attestation and bundle tests.
- Run live bypass regressions against isolated Gitea/Woodpecker infrastructure.
- Prove failure paths, not only clean merges.
- Record exact repo, PR, head SHA, bundle/policy digests and decision evidence.
- Distinguish local, mocked, live-pilot and production evidence.
- Validate secrets never enter ordinary build containers.
- Test idempotent onboarding and reconciliation across multiple repositories.
- Run backup/restore and upgrade/rollback rehearsals before production.
- Update SADRs, architecture, TODO and operator runbooks with each tranche.

## Recommended immediate action

Deploy the isolated trusted runner, then implement the exception/baseline path. Adding more scanners before those two
controls are operational creates breadth without resolving whether the merge decision is trustworthy and usable.
