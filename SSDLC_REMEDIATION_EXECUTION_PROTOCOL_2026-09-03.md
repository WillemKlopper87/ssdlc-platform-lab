# SSDLC Platform Lab — Remediation Execution Protocol

**Date:** 2026-09-03  
**Repository:** `WillemKlopper87/ssdlc-platform-lab`  
**Primary branch:** `main`  
**Review baseline:** `d21f93389111e00cc317792a0dcf9ba1365bc89c`  
**Purpose:** agent-ready remediation plan for converting the current strong SSDLC enforcement pilot into a trustworthy, independently enforced and auditable security control plane.

---

# 1. Executive position

This repository is no longer a basic scanner wrapper.

It already contains meaningful security engineering around:

- Gitea branch protection and review state;
- Woodpecker CI;
- Gitleaks, Semgrep and Trivy;
- severity normalisation;
- Rego/Conftest policy evaluation;
- current-head approval verification;
- retarget/stale-approval bypass protection;
- baseline/differential gating;
- vendored policy/rule integrity;
- bot approval separated from build containers;
- a trusted gate runner and containerised gate bundle;
- signed head-bound attestations;
- fail-closed report validation;
- onboarding/reconciliation automation;
- self-scanning and security ADRs.

Several findings from earlier reviews have already been fixed and must **not** be mechanically reopened.

The remaining problem is narrower and more important:

> **The platform must prove that a merge-authorising decision came from the exact independently approved gate implementation, on an isolated trust boundary, with complete retained evidence.**

The current code approaches that target, but the production trust property is not complete yet.

---

# 2. Agent operating protocol

For every finding:

1. Verify the current source and deployed state before editing.
2. Classify whether the issue affects `MERGE-INTEGRITY`, `RUNNER-ISOLATION`, `SUPPLY-CHAIN`, `EVIDENCE`, `GOVERNANCE`, or `OPERATIONS`.
3. Reproduce or write a proof-oriented regression test.
4. State the broken security invariant.
5. Identify the authoritative root cause.
6. Implement the smallest fix that strengthens the trust boundary.
7. Test bypass/failure paths, not only clean success.
8. Run unit/regression tests.
9. Run live isolated integration tests where the finding concerns Gitea/Woodpecker/runner behavior.
10. Record exact source SHA, policy/bundle digests and evidence for the validation.

Do not add scanners merely to increase coverage while the merge-authorisation trust path remains incomplete.

### Finding status

```text
OPEN
VERIFYING
CONFIRMED
IN PROGRESS
BLOCKED
FIXED — VALIDATION PENDING
DONE
DISPROVED
DEFERRED — ACCEPTED RISK
```

### Evidence status

```text
RUNTIME VERIFIED
STATIC VERIFIED
HIGHLY LIKELY
HYPOTHESIS
DESIGN RISK
DEPLOYMENT GAP
```

Severity and evidence confidence are separate.

---

# 3. Security invariants

## 3.1 Merge-authority invariant

A PR may become merge-eligible only when a trusted decision proves all of the following for the **current PR head**:

```text
repository identity
PR number
protected base/head relationship
head SHA
source/tree digest
trusted gate-bundle identity
policy/rules identity
scanner identities/versions
required scanner evidence
baseline/exception state
gate decision
trusted signer identity
```

A PR-controlled build must not be capable of manufacturing, modifying or replaying the evidence that authorises its own merge.

## 3.2 Signer-separation invariant

```text
signing authority != verification/approval authority
```

The bot/sidecar that verifies trusted gate decisions must not possess key material capable of creating those decisions.

## 3.3 Runner-isolation invariant

An untrusted PR may execute arbitrary build code, but that execution must not grant access to:

- host-root primitives;
- the Docker daemon controlling persistent platform infrastructure;
- gate signing keys;
- bot approval credentials;
- Gitea administrative credentials;
- evidence-store write authority outside its scoped job;
- mutable shared security databases that later trusted scans consume.

## 3.4 Scanner-evidence invariant

A required scanner is successful only if:

```text
scanner process executed
AND expected report exists
AND report is non-empty when format requires content
AND report parses to expected schema
AND report belongs to the current run/head
AND report digest is recorded
```

`exit code == 0` alone is not proof of scanner success.

## 3.5 Baseline invariant

- Baseline findings may only originate from an explicit onboarding/reset action.
- Refresh may only shrink the existing baseline.
- Secrets are never baselined.
- Corrupt/unreadable baseline state fails closed.
- A PR cannot update its own baseline and receive gate approval.

The current code substantially satisfies this invariant; preserve it.

## 3.6 Exception invariant

A real Critical/High finding may be bypassed only through a specific, time-bounded and auditable exception that requires independent authorised humans and automatically stops matching at expiry.

Changing global severity policy is not a per-finding exception mechanism.

## 3.7 Evidence invariant

Every merge/release decision must remain reconstructable without depending on ephemeral runner disks or CI logs.

---

# 4. Critical / P0 findings

## C-1 — The independently controlled trusted gate is built but not deployed as the required path

**Scope:** `MERGE-INTEGRITY`  
**Severity:** P0 / Critical release-control gap  
**Evidence:** DEPLOYMENT GAP, statically confirmed by current architecture documentation

The repository contains:

```text
scripts/trusted-gate-runner.py
gate-bundle/
gate_contract/
trusted attestation verification in bot-approver.py
```

But the current architecture still describes the trusted runner as **built, not deployed**, and `GATE_ATTESTATION_REQUIRED` remains a transitional opt-in rather than the authoritative path.

The live mechanism therefore still depends on PR-associated Woodpecker execution plus protected-file comparison.

That comparison is a valuable mitigation, but it is not equivalent to an independently administered gate.

### Broken invariant

```text
Untrusted application CI must be unable to manufacture the security evidence
that authorises its own merge.
```

### Required remediation

1. Provision a separately administered trusted-runner host/pool.
2. Give it only read access to source and write access to a dedicated evidence/attestation destination.
3. Install/release the gate bundle independently of application repos.
4. Require trusted attestation on exactly one pilot repo first.
5. Remove ordinary Woodpecker scan state from merge-authority decisions for that pilot.
6. Prove fail-closed behavior for:
   - no attestation;
   - invalid signature;
   - wrong head;
   - stale head after push;
   - wrong contract;
   - wrong policy;
   - wrong bundle;
   - required scanner missing;
   - scanner report malformed;
   - policy decision `fail`.
7. Retain the pass/fail evidence outside the runner.
8. Expand repo-by-repo only after the pilot is live and measured.

### Exit criterion

A malicious PR that completely controls its own Woodpecker pipeline must still be unable to earn the gate-bot approval without a valid independent attestation for the current head.

---

## C-2 — HMAC attestation gives the verifier/bot the power to forge trusted gate decisions

**Scope:** `MERGE-INTEGRITY`  
**Severity:** P0 / Critical trust-boundary flaw for production attestation  
**Evidence:** STATIC VERIFIED

The current attestation format uses HMAC-SHA256.

The same `GATE_ATTESTATION_KEY` is required by:

```text
trusted-gate-runner.py    -> signs
bot-approver.py           -> verifies
```

With HMAC, verification requires the signing secret.

Therefore compromise of the bot/verification process gives an attacker enough key material to create a syntactically valid “trusted runner” attestation without the trusted runner.

The source documentation correctly describes HMAC as a pilot bridge; it must not become the durable trust boundary.

### Broken invariant

```text
The merge-authorising verifier must not be able to mint the evidence it verifies.
```

### Required remediation

Move to asymmetric signing, for example:

```text
trusted gate signer
    -> private Ed25519/ECDSA key in isolated KMS/HSM/file identity

bot / verification service
    -> public verification key only
```

Prefer keyless/workload identity or hardware/KMS-backed signing where the deployment supports it.

The signed document should include a key ID/signing identity and support controlled rotation.

### Required tests

```text
test_verifier_has_no_private_signing_material
test_valid_signature_from_expected_key_passes
test_valid_signature_from_untrusted_key_fails
test_rotated_old_key_follows_defined_grace_policy
test_unsigned_attestation_fails
test_tampered_attestation_fails
```

---

## C-3 — Ordinary PR execution has host-root-equivalent Docker access before merge is denied

**Scope:** `RUNNER-ISOLATION`  
**Severity:** P0 / Critical production-runner risk  
**Evidence:** STATIC VERIFIED

The minimal Woodpecker agent mounts:

```text
/var/run/docker.sock:/var/run/docker.sock
```

The project itself documents this as root-equivalent and suitable only for the local lab/spike.

The key distinction is:

```text
bot protected-file comparison can stop a malicious .woodpecker.yml from MERGING
```

but it cannot stop that PR-controlled pipeline from **executing first** on the ordinary runner.

A malicious PR can therefore attack the runner host/control-plane before its eventual bot approval is refused.

### Broken invariant

```text
Untrusted PR execution must be disposable and unable to compromise persistent
SSDLC control-plane infrastructure.
```

### Required remediation

Production untrusted CI must use one of:

- disposable VM runners;
- isolated ephemeral Docker hosts;
- rootless/restricted remote builders;
- Kubernetes/VM sandbox runner pools with no control-plane credentials;
- equivalent one-job disposable infrastructure.

Separate at least:

```text
UNTRUSTED BUILD POOL
TRUSTED SECURITY GATE POOL
PLATFORM ADMIN/RECONCILIATION SERVICES
```

Never share signing credentials or platform-admin state across those boundaries.

### Required attack test

Create a canary PR whose pipeline deliberately attempts to:

- access Docker daemon;
- mount host filesystem;
- reach Gitea/Postgres/Woodpecker management endpoints;
- read other job volumes;
- access attestation/evidence stores.

The production runner profile must make those attempts fail even though the PR pipeline itself executes.

---

# 5. High / P1 findings

## H-1 — Signed attestation is not bound to the exact executable gate-bundle image digest

**Scope:** `MERGE-INTEGRITY`, `SUPPLY-CHAIN`  
**Severity:** P1, P0 candidate before production attestation rollout  
**Evidence:** STATIC VERIFIED

`gate-bundle/run-container.sh` correctly requires:

```text
GATE_BUNDLE_IMAGE=...@sha256:<digest>
```

which prevents a floating image reference at invocation time.

However, the image digest is not carried into `result.json`, the signed attestation, or the bot's expected-value checks.

The current signed identity includes:

```text
head SHA
contract digest
policy digest
decision
scanner success map
```

but not:

```text
bundle image digest
```

Therefore the system cannot prove from the attestation itself which executable bundle produced the decision.

### Required remediation

Add an operator-controlled expected bundle identity:

```text
gate_bundle_digest: sha256:...
```

The trusted runner must know the exact digest it invoked and include it in the signed document.

The verifier must compare it to the configured/released expected digest.

Also include a bundle release/version and preferably provenance reference.

### Tests

```text
test_expected_bundle_digest_passes
test_wrong_bundle_digest_fails
test_missing_bundle_digest_fails
test_bundle_digest_is_signed
```

---

## H-2 — Report digests are computed but discarded before attestation/evidence retention

**Scope:** `EVIDENCE`, `MERGE-INTEGRITY`  
**Severity:** P1  
**Evidence:** STATIC VERIFIED

The trusted bundle now correctly validates each required scanner report and computes `report_digests`.

That was an important remediation.

But `trusted-gate-runner.py::issue()` does not copy `report_digests` into the signed attestation.

The runner workspace/output lives in a temporary directory that is deleted after processing.

Thus the strongest current artifact proving what each scanner actually emitted disappears while only a coarse scanner status remains.

### Required remediation

The signed attestation should contain at least:

```text
report digest per scanner
normalised-findings digest
scanner version
scanner DB/rules version where relevant
baseline digest
```

Persist raw scanner reports and normalized decision input in immutable/append-only evidence storage outside the runner.

### Required evidence chain

```text
source/head
 -> raw report hashes
 -> normalized findings hash
 -> policy input/output
 -> signed gate decision
```

---

## H-3 — Every ordinary PR step receives writable access to the shared Trivy DB cache

**Scope:** `RUNNER-ISOLATION`, `SUPPLY-CHAIN`  
**Severity:** P1  
**Evidence:** STATIC VERIFIED

The Woodpecker agent config sets:

```text
WOODPECKER_BACKEND_DOCKER_VOLUMES=
    ssdlc-minimal-trivy-db-cache:/root/.cache/trivy
```

and the repository comments explicitly note that this volume is injected into **every** step container.

The mount is not declared read-only.

The normal dependency scan later uses the shared cache and frequently runs with `--skip-db-update`.

Therefore an untrusted PR step can potentially modify/delete/poison vulnerability DB state consumed by subsequent jobs.

Even if a malicious pipeline change cannot merge, it may have already altered shared security infrastructure used by other repositories.

### Broken invariant

```text
Untrusted application code may consume security intelligence but may not mutate
the authoritative shared security-intelligence cache.
```

### Required remediation

Preferred design:

```text
DB refresh service -> writable authoritative cache
                     -> verified snapshot/digest
                     -> scan jobs receive read-only copy/mount
```

Alternative: per-job disposable cache copied from a verified seed.

Add freshness and integrity metadata checks before scanning.

### Required tests

```text
test_untrusted_step_cannot_write_trivy_db
test_corrupt_cache_fails_closed
test_stale_db_fails_policy_or_alerts_per_SLA
test_refresh_publishes_verified_snapshot_atomically
```

---

## H-4 — Transitional fast gate still treats a missing scanner report as optional input

**Scope:** `MERGE-INTEGRITY`  
**Severity:** P1 while fast-gate mode remains merge-authoritative  
**Evidence:** STATIC VERIFIED

The trusted bundle fixed the earlier report-validation problem by explicitly validating each report.

The reusable `evaluate-findings.py`, however, still intentionally treats a missing report as “scanner not supplied” and simply collects no findings from it.

That is reasonable for a generic evaluator with optional inputs.

It is weaker when the transitional Woodpecker gate requires all three scanners.

If a scanner step were to return exit 0 without actually creating its expected report, the pipeline step can remain green while the evaluator silently treats that scanner as absent.

### Required remediation

Do not change the generic evaluator's optional-input semantics blindly.

Instead, make the fast pipeline run a required-evidence validator before policy evaluation, or add a strict mode:

```text
--require gitleaks
--require semgrep
--require trivy
```

where missing/empty/invalid required reports produce exit 2.

Use the same report-shape validation contract as the trusted bundle.

---

## H-5 — Transitional enforcement downloads executable security tooling without immutable verification

**Scope:** `SUPPLY-CHAIN`  
**Severity:** P1  
**Evidence:** STATIC VERIFIED

Two notable paths remain in the fast gate:

### Reviewdog

The inline-comment step executes:

```text
https://raw.githubusercontent.com/reviewdog/reviewdog/master/install.sh
```

and supplies a Gitea API token to that CI step.

Although a fixed Reviewdog version is passed to the installer, the installer script itself comes from mutable `master`.

### Conftest

The policy-evaluation step downloads the Conftest release tarball by version URL and executes it without verifying a checksum/signature.

Conftest is directly in the blocking policy path.

### Required remediation

- Pin installer/source scripts to immutable commit SHAs or remove curl-pipe-shell installation entirely.
- Verify published checksums/signatures before executing downloaded binaries.
- Prefer prebuilt internally approved images by digest.
- Ensure token-bearing reporting steps use minimal-scope credentials.

This becomes less security-critical after merge authority moves to the independent trusted gate, but should still be fixed because ordinary CI remains a security-sensitive service.

---

## H-6 — `GATE_BUNDLE_COMMAND` is documented as absolute/platform-owned but the runner does not enforce it

**Scope:** `MERGE-INTEGRITY`  
**Severity:** P1 hardening  
**Evidence:** STATIC VERIFIED

`trusted-gate-runner.py` runs:

```python
subprocess.run(shlex.split(command), cwd=workspace, ...)
```

The docstring says `GATE_BUNDLE_COMMAND` must be an absolute operator-owned executable.

The code does not enforce that.

Because `cwd` is the untrusted PR workspace, a relative executable/path creates a dangerous configuration footgun: PR content can shadow what the operator thought was a trusted command.

### Required remediation

Parse the command and require:

- executable path is absolute;
- executable resolves outside `GATE_WORKSPACE`;
- executable ownership/permissions meet policy;
- optionally executable digest matches configured expected value.

Prefer a fixed runner configuration object rather than arbitrary command text.

### Test

```text
test_relative_gate_bundle_command_rejected
test_workspace_executable_rejected
test_expected_absolute_wrapper_accepted
```

---

## H-7 — Trusted runner passes almost the entire host environment to the scanner bundle

**Scope:** `RUNNER-ISOLATION`, `SECRETS`  
**Severity:** P1 hardening  
**Evidence:** STATIC VERIFIED

The runner removes several known credentials before spawning the bundle:

```text
GATE_ATTESTATION_KEY
GITEA_RUNNER_TOKEN
GITEA_BOT_TOKEN
WOODPECKER_TOKEN
```

but otherwise forwards the host environment.

This is denylist-based secret isolation.

A future credential such as cloud, registry, Vault, proxy-auth or monitoring tokens could be exposed merely because its name was not added to this list.

### Required remediation

Build the child environment from an explicit allowlist:

```text
PATH or fixed executable paths
GATE_WORKSPACE
GATE_OUTPUT_DIR
GATE_HEAD_SHA
GATE_PR_NUMBER
GATE_REPOSITORY_OWNER
GATE_REPOSITORY_NAME
required locale/runtime variables only
```

The scanner container already runs without network and credentials; preserve that property at the process boundary too.

---

## H-8 — No controlled two-party exception workflow exists

**Scope:** `GOVERNANCE`  
**Severity:** P1 before broad organisational rollout  
**Evidence:** DEPLOYMENT GAP / design explicitly says not built

Baseline/differential gating is now implemented and should not be confused with exceptions.

What is still absent is the designed flow for a **new, real** Critical/High finding that cannot immediately be fixed.

Current practical choices are:

```text
fix the finding
OR do not merge
OR change global platform policy out-of-band
```

That is not a sustainable risk-acceptance mechanism for broad adoption.

### Required remediation

Implement the existing design:

- stable finding ID/fingerprint;
- justification and ticket;
- owner;
- hard maximum expiry;
- Security Officer approval;
- accountable Engineering Lead approval;
- two distinct humans;
- no PR-author self approval;
- signed/protected exception artifact;
- automatic re-block at expiry;
- append-only evidence;
- quarterly review/reporting.

Keep **false-positive suppression** separate from **accepted real risk**.

---

## H-9 — Secret detection still occurs after Git has accepted the object

**Scope:** `SECRETS`  
**Severity:** P1  
**Evidence:** DEPLOYMENT GAP

Gitleaks in CI/trusted gate can prevent merge, but the secret may already exist in:

- Gitea object storage;
- clones;
- webhook payloads;
- runner workspaces;
- logs;
- backups.

The pre-receive experiment exists conceptually but is not installed as the normal onboarding control.

### Required remediation

Install a chained pre-receive secret scanner that:

- scans introduced objects before acceptance;
- cannot be configured by the incoming branch;
- uses suppressions from protected platform/default-branch policy;
- has measured latency;
- fails according to an explicit outage policy;
- triggers credential-rotation/incident guidance on detection.

Secrets should still be rescanned in CI; pre-receive is an additional earlier boundary, not a replacement.

---

## H-10 — Platform onboarding/admin identity is a standing direct-push bypass

**Scope:** `GOVERNANCE`, `SECRETS`  
**Severity:** P1  
**Evidence:** STATIC VERIFIED

`onboard-repo.sh` intentionally configures branch protection with:

```text
enable_push=true
enable_push_whitelist=true
push_whitelist_usernames=[automation user]
```

and explicitly documents that whoever holds `GITEA_ADMIN_TOKEN` has a standing review bypass on every onboarded repo.

The same automation also:

- pushes managed gate files directly to `main`;
- pushes the vendored rules tree;
- manages collaborators/branch protection.

### Required remediation

Split identities and duties:

```text
platform onboarding/admin
policy release publisher
read-only policy verifier
gate bot approver
trusted source fetcher
evidence writer
```

Prefer temporary/restricted elevation for onboarding rather than a standing broad PAT.

Audit direct protected-branch pushes by automation and require an immutable release/source reference for every managed-file update.

---

# 6. Medium / P2 findings

## M-1 — Gate-bundle base images are version-tag pinned, not digest pinned

The runtime bundle is invoked by immutable digest, which is strong.

However, the Dockerfile's upstream stages use mutable registry tags such as:

```text
zricethezav/gitleaks:v8.30.1
aquasec/trivy:0.74.0
openpolicyagent/conftest:v0.69.0
semgrep/semgrep:1.174.0
```

A rebuild at the same source commit is not guaranteed to consume identical base image bytes.

Pin build inputs by digest, produce an SBOM/provenance record for the gate bundle itself, and sign/release that bundle independently.

---

## M-2 — Bot approver and reconciliation remain manual, single-repository processes

The scripts are thoughtful and idempotent, but the current architecture still describes them as manually started and single-repository per process.

For broad rollout, convert them into a supervised multi-repository service with:

- webhook ingest;
- durable queue;
- periodic reconciliation;
- idempotency by repo/PR/head;
- retry/dead-letter handling;
- rate-limit handling;
- health endpoints;
- per-repo configuration;
- audit/evidence output.

The service must not gain the private gate signing key.

---

## M-3 — Scanner/DB freshness needs an enforceable SLA

A Trivy DB refresh pipeline exists but its header says registration is still an operator action.

Production should define:

```text
last successful refresh
expected refresh cadence
max acceptable age
failure alert
scan behavior when stale
```

A stale security database should be visible in evidence and policy, not only logs.

---

## M-4 — Production operational profile remains incomplete

The current minimal profile is deliberately a lab profile.

Before production usage, prove:

- TLS/reverse proxy/identity integration;
- encrypted secret management (SOPS/Vault/KMS equivalent);
- backup and restore;
- upgrade/rollback;
- monitoring/log aggregation;
- evidence-store recovery;
- capacity and multi-repo behavior;
- runner replacement after compromise;
- defined RTO/RPO.

---

# 7. Previously reported findings that are now closed

The remediation agent should preserve these controls and add regression coverage rather than re-implement them from scratch.

## CLOSED-A — Corrupt existing baseline could become a fresh acceptance snapshot

Current `generate-baseline.py` distinguishes genuinely missing baseline from unreadable/invalid existing baseline and requires explicit `--create` for first creation.

`onboard-repo.sh` now distinguishes HTTP 404/200/errors and no longer swallows decode failures.

**Status:** CLOSED / regression-protect.

## CLOSED-B — Trusted bundle labelled scanners successful without proving reports existed

Current trusted bundle validates required report files for existence, non-empty content, JSON shape and computes report hashes before setting scanner status to `success`.

**Status:** CLOSED, but H-2 remains because report digests are not carried into signed retained evidence.

## CLOSED-C — Trusted bundle silently ignored repository baseline / inconsistent Semgrep root

Current trusted bundle passes explicit:

```text
--baseline <workspace>/.ssdlc/baseline.json
--semgrep-root <workspace>
```

**Status:** CLOSED / regression-protect.

## CLOSED-D — Gitleaks was ineffective against archive checkout with no `.git`

Trusted bundle now uses `gitleaks detect --no-git` for the archive-based workspace.

**Status:** CLOSED / regression-protect.

## CLOSED-E — Gate-bundle container ran as root

Current Dockerfile drops to the existing `semgrep` non-root user and the wrapper also uses `--cap-drop ALL`, read-only filesystem and no network.

**Status:** CLOSED / regression-protect.

---

# 8. Required new invariant/regression tests

Suggested tests:

```text
tests/trust/test_attestation_signer_separation.py
tests/trust/test_bundle_digest_binding.py
tests/trust/test_report_evidence_binding.py
tests/trust/test_gate_command_is_platform_owned.py
tests/trust/test_runner_environment_allowlist.py

tests/runner/test_untrusted_runner_isolation.py
tests/runner/test_trivy_cache_read_only.py

tests/gate/test_required_reports_fail_closed.py
tests/gate/test_scanner_database_freshness.py

tests/governance/test_exception_two_party.py
tests/governance/test_exception_expiry_reblocks.py

tests/secrets/test_pre_receive_secret_rejection.py
```

### Canary repository

Maintain a continuously exercised canary proving:

```text
clean PR passes
new Critical/High blocks
Medium/Low warns per policy
secret push rejected before object acceptance
managed pipeline tamper rejected
vendored-rules tamper rejected
missing scanner report blocks
wrong bundle digest blocks
wrong signer blocks
missing/stale attestation blocks
stale head attestation blocks
valid baseline finding does not block
reintroduced fixed finding blocks
valid exception passes only until expiry
expired exception re-blocks
```

---

# 9. Recommended execution order

## Phase 0 — Freeze the production trust model

Do not onboard broad developer populations yet.

Document that:

```text
compose/minimal = lab only
GATE_ATTESTATION_REQUIRED=0 = transitional pilot mode
HMAC attestation = pilot only
```

## Phase 1 — Independent gate authority

1. Replace HMAC with asymmetric signing.
2. Add expected signer identity/key ID.
3. Add gate-bundle image digest to result + attestation + verifier.
4. Add report/normalised-input digests to signed evidence.
5. Release the gate bundle independently with SBOM/provenance/signature.
6. Deploy one isolated trusted runner.
7. Enable required attestation for one canary/pilot repo.

### Phase 1 exit

```text
bot/verifier compromise alone cannot forge a gate decision
AND PR-controlled CI cannot create gate evidence
AND exact approved gate bundle is provable from attestation
```

## Phase 2 — Isolate untrusted builds

1. Remove production use of host Docker socket from persistent control-plane host.
2. Create disposable/rootless untrusted runner pool.
3. Make shared security-intelligence caches read-only to PR jobs.
4. Restrict untrusted job networking to required destinations.
5. Continuously attack-test the isolation boundary.

## Phase 3 — Durable evidence

Persist outside runners:

```text
source/head digest
raw reports
normalised findings
baseline digest
contract digest
policy digest
bundle digest
scanner versions/DB freshness
attestation
bot/human approvals
merge commit
```

Add per-PR/per-release evidence export and retention/backup policy.

## Phase 4 — Usable enforcement

1. Build two-party exceptions.
2. Build false-positive suppressions separately.
3. Enforce expiry/re-block.
4. Measure developer wait time and exception SLA.
5. Keep human code review distinct from security risk acceptance.

## Phase 5 — Secrets before storage

1. Package pre-receive secret detection.
2. Add protected suppressions.
3. Benchmark latency.
4. Exercise scanner outage/fail-safe policy.
5. Integrate credential-rotation incident guidance.

## Phase 6 — Service scale

1. Supervise bot + reconciliation.
2. Multi-repo durable queue.
3. Health/metrics/alerts.
4. Scoped service identities.
5. OIDC/team governance mapping.

## Phase 7 — Artifact trust

For release workloads:

```text
build once
-> SBOM
-> scan actual image digest
-> provenance
-> sign
-> controlled registry
-> promote same digest
-> verify at deployment
```

Do not rebuild between environments.

## Phase 8 — Operations and portal

Only after the trust path is proven:

- production TLS/edge;
- secret management;
- backup/restore drills;
- monitoring;
- capacity testing;
- audit exports;
- thin developer/operator portal.

The portal must aggregate evidence; it must not become a second security system of record.

---

# 10. Remediation ledger

| ID | Scope | Finding | Severity | Evidence | Status |
|---|---|---|---:|---|---|
| C-1 | Merge integrity | Independent trusted gate built but not deployed/required | P0 | Deployment gap | OPEN |
| C-2 | Merge integrity | HMAC verifier possesses signing authority | P0 | Static verified | OPEN |
| C-3 | Runner isolation | PR-controlled Woodpecker execution has Docker-socket host-root power | P0 | Static verified | OPEN |
| H-1 | Merge/supply chain | Attestation not bound to bundle image digest | P1 / P0 candidate | Static verified | OPEN |
| H-2 | Evidence | Report digests computed but not signed/retained | P1 | Static verified | OPEN |
| H-3 | Runner/supply chain | Shared Trivy DB cache writable by every PR step | P1 | Static verified | OPEN |
| H-4 | Merge integrity | Transitional fast gate lacks strict required-report mode | P1 | Static verified | OPEN |
| H-5 | Supply chain | Mutable/unverified tool bootstrap in transitional gate | P1 | Static verified | OPEN |
| H-6 | Merge integrity | Absolute platform-owned gate command is convention, not enforced | P1 | Static verified | OPEN |
| H-7 | Secrets | Trusted runner uses environment denylist rather than allowlist | P1 | Static verified | OPEN |
| H-8 | Governance | Two-party exceptions not built | P1 | Deployment gap | OPEN |
| H-9 | Secrets | Secret rejection occurs after Git object acceptance | P1 | Deployment gap | OPEN |
| H-10 | Governance | Admin automation has standing protected-branch push bypass | P1 | Static verified | OPEN |
| M-1 | Supply chain | Bundle Dockerfile base images not digest-pinned | P2 | Static verified | OPEN |
| M-2 | Reliability | Bot/reconciliation manual and single-repo | P2 | Deployment gap | OPEN |
| M-3 | Scanner intel | Trivy DB freshness not an enforced SLA | P2 | Design/deployment | OPEN |
| M-4 | Operations | Production recovery/monitoring/secrets profile incomplete | P2 | Deployment gap | OPEN |

---

# 11. Definition of done

A finding is not complete because code changed.

For P0/P1 findings:

```text
VERIFY CURRENT STATE
-> WRITE FAILING SECURITY REGRESSION
-> IDENTIFY ROOT CAUSE
-> IMPLEMENT FIX
-> UNIT TEST
-> BYPASS/FAILURE TEST
-> LIVE ISOLATED INTEGRATION TEST IF APPLICABLE
-> FULL UNIT/REGRESSION SUITE
-> SELF-SCAN CHANGED PLATFORM ARTIFACTS
-> RECORD EXACT DIGESTS/SHA
-> UPDATE ADR/ARCHITECTURE/OPERATIONS
-> RETAIN VALIDATION EVIDENCE
-> DONE
```

For trust-path changes, `DONE` requires live evidence from an isolated environment; mocked/unit-only proof is insufficient.

---

# 12. Final target architecture

```text
                    PLATFORM CONTROL PLANE

Gitea ---------------------------------------------------------+
  |                                                            |
  | PR source/head                                             | human review
  v                                                            v
UNTRUSTED BUILD POOL                                      protected branch
  disposable/rootless                                           ^
  no signing keys                                                |
  no host Docker control                                        |
  no evidence authority                                         |
                                                               bot approval
                                                                ^
                                                                |
ISOLATED TRUSTED GATE                                           |
  read exact head                                                |
  approved bundle @ immutable digest                             |
  read-only verified scanner DB/rules                            |
  scan + policy                                                  |
  retain raw evidence                                            |
  sign with PRIVATE asymmetric key ------------------------------+

VERIFIER/BOT
  PUBLIC verification key only
  verifies head + signer + contract + policy + bundle + reports
  cannot forge trusted decision
```

The guiding principle is:

> **A secure SDLC platform is only as strong as the evidence that authorises the merge. The application PR, ordinary CI runner, and verification bot must each be unable to manufacture that evidence alone.**
