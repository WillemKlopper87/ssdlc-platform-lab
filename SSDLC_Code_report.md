# SSDLC Platform codebase critique — 28 August 2026

## 1. Executive summary

This review assessed the current SSDLC Platform Lab codebase at commit `bb8cd53`, including Gitea/Woodpecker topology, onboarding, merge-gate trust boundaries, scanner policy, normalisation, baseline gating, approval verification, trusted-runner/attestation code, regression harnesses, infrastructure definitions, operational documentation and roadmap.

The project has advanced materially since the previous critique. Baseline/differential gating is now implemented and live-verified, the vendored-rules bypass regression has been proven against a real stack, private repository onboarding has live evidence, the trusted bundle is aligned to the fast-gate policy digest, and a silently inert Gitleaks invocation in the archive-based trusted runner was found and fixed. These are strong, concrete security improvements.

The core conclusion is nevertheless unchanged: this remains an enforcement pilot, not a production SSDLC control plane. The durable trust boundary—a separately administered runner producing an independently signed, head-bound decision—is built in code but not deployed. Required attestation remains disabled, the HMAC key is a pilot bridge, and ordinary PR execution retains host-root-equivalent Docker socket access.

This round also identified two fresh integrity risks in the new baseline/attestation paths:

1. an unreadable existing baseline is treated as “no baseline” during refresh, causing all current findings to be accepted into a fresh baseline; this can violate the promised shrink-only property;
2. the trusted gate bundle declares all scanners successful without explicitly verifying that each expected report exists and is structurally valid before writing `result.json`.

These should be fixed before required attestation is enabled for a pilot repository.

### Overall assessment

| Area | Assessment | Main conclusion |
|---|---|---|
| Threat model | Strong | Bypass classes are explicit and unusually well tested. |
| Fast PR gate | Good pilot | Secrets, SAST and dependency findings are unified and differential gating now works. |
| Merge trust boundary | Critical gap | Trusted runner is not deployed; transitional protected-file comparison remains authoritative. |
| Attestation design | Promising, not production-ready | Head/policy/contract binding exists; HMAC and report-validation gaps remain. |
| Developer usability | Early | Native Gitea/Woodpecker feedback and scripts work, but exception/remediation workflows are incomplete. |
| Evidence/compliance | Weak | No durable evidence store, audit-pack export or retention implementation. |
| Operations | Weak | No monitoring, encrypted secret workflow, automated backup/restore or proven upgrade/rollback. |
| Test quality | Strong locally | Unit and live regressions are thoughtful; trusted isolated execution is not a standing automated service. |

No existing source or documentation files were modified. The only added file from this review is this report.

## 2. Repository and review baseline

### Current state

- Working directory: `C:\applications\SSDLC-Platform-Lab`.
- Actual Git top level: `C:\applications`.
- Git prefix: `SSDLC-Platform-Lab/`.
- Branch: local `master` at `bb8cd53`.
- No Git remote is configured.
- The parent repository sees sibling application directories as untracked content.
- Approximately 117 Python files, 19 shell scripts, 608 YAML files and 4 Terraform files are present in the project directory; much of the YAML count is vendored rules and fixtures.

### Review method

The review covered:

- current code and recent commits;
- the existing critique, TODO, roadmap, SADRs and operational documents;
- fast-gate and trusted-bundle scanner execution;
- approval, protected-tree and attestation verification;
- baseline generation and refresh semantics;
- private-repository onboarding and credential handling;
- Compose, Docker socket and platform-secret posture;
- test coverage and live-evidence boundaries;
- deployment/rollback documentation and missing operational automation.

This is an engineering and security-design critique. It is not a penetration test, formal licensing opinion, live production assessment or audit certification.

## 3. Validation performed in this review

The complete local unit tier passed:

- shell and Python syntax checks;
- Woodpecker interpolation lint;
- 20 approval-policy assertions;
- 17 finding-normalisation assertions;
- 18 findings-evaluator assertions;
- 5 bot-approver tests;
- 5 gate-attestation tests;
- 4 trusted-runner tests;
- 3 gate-bundle tests;
- private-onboarding source validation;
- 3 baseline-generator tests;
- 5 Conftest/Rego tests.

These results are good evidence for deterministic local logic. They do not prove that the trusted runner is deployed, isolated, continuously operating or independently administered. This review did not rerun the Docker-dependent live forgery/minimal suites, register Woodpecker cron jobs, execute Ansible on POSIX, or perform backup/restore.

## 4. What has improved since the prior critique

### Baseline/differential gating is now real

The prior critique said baseline workflow was absent. That statement is stale. SADR-0024 introduced:

- onboarding-time baseline generation;
- Semgrep and Trivy stable fingerprints;
- exclusion of secrets from baselining;
- self-shrinking refresh through intersection with current findings;
- protection of `.ssdlc/baseline.json` as a managed gate file;
- fast-gate policy evaluation of introduced/reintroduced findings;
- live verification of the flow.

The two-party exception workflow remains absent and should not be conflated with baseline completion.

### Vendored-rule and policy integrity are stronger

The bot now compares the entire vendored-rules tree using a recursive Git Trees API response and fails closed on missing, malformed, empty, changed or truncated data. Trusted attestations carry a content-derived policy digest, and the bot verifies the operator-configured expected digest.

### Trusted-bundle secret detection is no longer silently inert

The archive-based trusted workspace has no `.git` directory. Adding `gitleaks detect --no-git` was essential; without it, zero commits and zero findings were reported regardless of file content. The fix is unit-tested and was live-verified against a rebuilt bundle.

### Live evidence is broader

The project now records live proof for:

- vendored-rule tamper rejection;
- stale bot/human approvals;
- private repository onboarding and idempotent rerun;
- policy-digest binding;
- baseline stability;
- Gitleaks operation in the real trusted-workspace shape;
- reconciliation of a missed webhook/stuck PR.

This evidence discipline is a major strength.

## 5. Critical findings

### C1 — The durable trusted gate boundary is not deployed

**Severity: Critical — merge-authorisation trust**

The trusted bundle, runner, result contract and attestation verification exist. The runner is not on a separately controlled host, required attestation is not enabled, and the bot still relies on transitional protected-base comparisons plus Woodpecker step state.

The current transitional control is useful and demonstrably catches known tampering. It cannot provide the stronger property that PR-controlled execution is incapable of manufacturing the evidence that authorises its own merge.

Required remediation:

1. Provision a separately administered trusted-runner host or equivalent isolation boundary.
2. Install the gate bundle by immutable digest, independently of application repositories.
3. Restrict network access to source fetch, vulnerability databases, evidence storage and the narrow control APIs required.
4. Keep Gitea/Woodpecker/bot administrative credentials out of scanning containers.
5. Require a current-head attestation for one pilot repository only.
6. Prove clean pass, High/secret block, pipeline/policy tampering, missing attestation, invalid signature, stale head and scanner failure.
7. Retain pass and fail evidence before expanding rollout.

Do not enable `GATE_ATTESTATION_REQUIRED=1` broadly merely because unit tests pass.

### C2 — Ordinary Woodpecker execution is host-root equivalent

**Severity: Critical — runner host compromise**

`compose/minimal/docker-compose.yml` mounts `/var/run/docker.sock` into the Woodpecker agent. Any pipeline step able to control Docker through that socket can obtain root-equivalent control over the host and reach persistent platform data. The Compose comments correctly describe this as acceptable only for the local spike.

Required remediation:

- use disposable/dedicated runner hosts for untrusted builds;
- separate trusted and untrusted runner pools;
- avoid a shared control-plane Docker socket through rootless/remote builder or ephemeral VM patterns;
- prevent untrusted jobs reaching Gitea/Woodpecker administrative endpoints, evidence storage and signing services;
- rotate or destroy runner hosts after suspected compromise;
- document that the minimal profile is not a production deployment option.

## 6. High-priority findings

### H1 — Baseline refresh can violate the shrink-only guarantee

**Severity: High — enforcement integrity**

`scripts/generate-baseline.py::load_existing()` returns `None` for a missing baseline and also for an unreadable, malformed or structurally invalid existing baseline. `main()` treats `None` as permission to create a fresh baseline containing every current Semgrep/Trivy finding.

During re-onboarding, `scripts/onboard-repo.sh` fetches the current baseline, decodes it with `base64 -d ... || true`, and invokes the generator. A transient API response problem, bad base64, corrupt file or schema error can therefore turn a shrink-only refresh into a fresh acceptance snapshot. Newly introduced High/Critical findings present at that moment become baseline debt instead of blocking.

This is asymmetric with evaluation, where malformed baseline data is safely treated as empty and all findings become new.

Recommendation:

- distinguish “baseline genuinely absent on first onboarding” from “baseline exists but cannot be read/validated”;
- fail closed on malformed/unreadable existing content;
- require an explicit operator action such as `--create-new-baseline` for first creation or deliberate reset;
- validate schema version, repository identity, base commit and unique typed entries;
- remove `base64 -d ... || true` from the security-sensitive path and propagate decoding failure;
- add a regression test proving corrupt existing baselines cannot be replaced automatically;
- audit and protect deliberate baseline resets as exceptional administrative events.

### H2 — Trusted-bundle scanner success is asserted rather than proven from report artifacts

**Severity: High — attestation integrity**

`gate-bundle/run-pilot-bundle.py` runs Gitleaks, Semgrep and Trivy and checks their exit codes. It then calls the evaluator and, if evaluation returns 0 or 1, writes:

```json
"scanners": {"secrets":"success","sast":"success","dependencies":"success"}
```

There is no explicit pre-attestation check that each expected report exists, is non-empty, matches the expected JSON structure and corresponds to the current workspace. The evaluator intentionally treats a missing report path as “no findings,” which is useful for optional fast-gate inputs but unsafe when generating a signed claim that a required scanner succeeded.

A scanner that exits zero without producing its report—through a tool regression, unexpected filesystem behaviour or wrapper mistake—can therefore be labelled successful.

Recommendation:

- after every scanner invocation, require the report file to exist, be a regular file, remain inside the output directory and parse to the scanner's expected top-level structure;
- include report hashes and scanner versions in `result.json` and the signed attestation;
- make required scanner names input to the bundle contract rather than hardcoded success labels;
- test “exit 0, no report,” empty report, malformed report, stale pre-existing report and output-path substitution;
- clear/create an exclusive output directory before each run and write results atomically.

### H3 — Trusted-bundle baseline semantics are not explicitly bound

**Severity: High-medium — policy consistency and availability**

The fast gate evaluates `.ssdlc/baseline.json` through the evaluator's default relative path because it runs in the repository workspace. The trusted bundle invokes the evaluator with absolute scanner report paths but does not pass `--baseline <workspace>/.ssdlc/baseline.json` or set its working directory explicitly.

Depending on the container's current working directory, the trusted decision can ignore the repository baseline and treat inherited High/Critical findings as new. This is more likely to over-block than bypass, but it makes fast and trusted decisions inconsistent and undermines the promise that the attestation represents the same approved policy.

Recommendation:

- pass the workspace baseline path explicitly;
- decide whether baseline content is part of the attested decision input and include its hash;
- require the baseline to match the protected base revision, not merely the PR checkout;
- test identical fast/trusted outcomes for inherited, fixed, introduced and reintroduced findings.

### H4 — No usable two-party exception workflow exists

**Severity: High — enforcement survivability and governance**

Baseline gating makes legacy onboarding usable but does not handle false positives, temporarily accepted new risk or compensating controls. The designed exceptions repository, separate suppressions, expiry, Security Officer approval and accountable Engineering Lead approval are not implemented.

Without a controlled path, operational pressure tends to produce unsafe alternatives: disabling the gate, editing policy, removing branch protection or granting an administrative bypass.

Recommendation:

- separate false-positive suppression from risk acceptance;
- require stable finding identity, repository, justification, owner, ticket and maximum expiry;
- require two distinct authorised roles and preserve peer review separately;
- make expiry automatically re-block;
- make exception data immutable/append-only in evidence and readable by the gate from a protected source;
- report exception age, SLA and review cadence.

### H5 — Secret detection still occurs after Git accepts objects

**Severity: High — credential exposure**

The fast and trusted gates can block merge, but the secret is already present in Gitea, pipeline workspaces, logs, mirrors and developer clones. The pre-receive Gitleaks experiment is not installed as a chained Gitea hook.

Recommendation:

- package the hook as an Ansible-managed extension that chains after Gitea's generated hook;
- scan introduced objects before acceptance;
- read suppressions from the protected default branch or centrally managed policy, never from the incoming push;
- benchmark realistic repository sizes and set a latency budget;
- provide a break-glass process with audit and immediate credential rotation;
- test force pushes, tags, large packs, deletes, binary content and scanner outage.

### H6 — Evidence retention and export are absent

**Severity: High — auditability**

Woodpecker logs and local attestation JSON are not a durable evidence system. A production record must connect:

- repository and risk tier;
- PR, protected base and exact head SHA;
- source/tree digest;
- gate contract, bundle and policy/rule digests;
- scanner versions and vulnerability DB versions;
- raw and normalised findings;
- baseline and exception state;
- human and bot approvals;
- merge commit;
- built artifact, SBOM, provenance, signature and deployment.

Recommendation:

- store immutable or append-only evidence outside the runner;
- hash/sign records and enforce least-privilege access;
- define retention, legal hold, deletion and backup policy;
- create per-PR, per-release and audit-period exports;
- retain failing decisions and unavailable-scanner events, not only successful merges.

## 7. Medium-priority findings

### M1 — Repository root includes unrelated sibling projects

**Severity: Medium-high — accidental scope and supply-chain risk**

The Git top level is `C:\applications`, while this project is a subdirectory. Sibling repositories appear as untracked content. This creates risks of accidental staging, wrong scan scope, misleading status checks and automation operating on an unintended working tree. No remote is configured, so there is also no current upstream/promotion path for the platform itself.

Recommendation:

- migrate deliberately to a dedicated repository rooted at `SSDLC-Platform-Lab`;
- preserve history and tags, and coordinate before rewriting or relocating shared state;
- configure an authoritative remote and protected default branch;
- add a preflight that refuses to operate if the detected Git root is not the expected project root.

### M2 — Bot approval remains single-repository and manually operated

**Severity: Medium-high — reliability and scale**

`bot-approver.py` is environment-configured for one repository and is not a supervised Compose service. The reconciliation loop is also standalone. There is no durable queue, webhook service, retry/dead-letter handling, per-repository configuration store or multi-repository health model.

Recommendation:

- consolidate bot approval and reconciliation into a supervised service;
- consume webhooks into a durable queue and reconcile periodically;
- make actions idempotent by repo/PR/head;
- handle Gitea/Woodpecker rate limits and outages;
- expose health, queue age and last-success metrics;
- retain every decision reason as evidence.

### M3 — Administrative identities are broader than their tasks

**Severity: Medium-high**

Onboarding relies on `GITEA_ADMIN_TOKEN`, which can push managed files and configure repositories. The code comments correctly acknowledge that this token is a standing review bypass. Some read-only policy checks also require repository-admin permission because of Gitea API behaviour, even if the token scope itself is read-only.

Recommendation:

- separate onboarding, read-policy, commenting, approval, reconciliation and platform-administration identities;
- scope tokens per organisation/repository and rotate them;
- store secrets encrypted, inject at runtime and never expose approval/signing credentials to build containers;
- monitor admin collaborator grants and token use;
- resolve LDAP/OIDC group ownership before relying on named governance roles.

### M4 — Platform secrets remain plaintext on disk

**Severity: Medium-high**

`.env` and `.gitea_admin_token` are ignored but plaintext. The planned SOPS/age flow is not implemented. On a single Docker host that already exposes a root-equivalent socket, local secret files increase blast radius.

Recommendation:

- implement SOPS/age or an external secret store;
- separate deploy-time, runtime and signing secrets;
- use file/secret mounts rather than broad process environments where possible;
- rotate credentials after provisioning and suspected runner compromise;
- ensure backups do not silently capture plaintext secrets.

### M5 — Trivy DB refresh exists but is not scheduled or governed

**Severity: Medium**

The shared cache and refresh pipeline are implemented. The Woodpecker cron is not registered, with no owner, freshness SLA or alert. The fast gate self-primes an empty cache, which prevents total failure but does not provide predictable database freshness.

Recommendation:

- register the cron deliberately;
- record DB metadata/version in every decision;
- alert on age and failed refresh;
- test registry outage and rate limiting;
- define whether stale DB age blocks, warns or invokes an explicit degraded mode.

### M6 — Scanner and policy coverage remains incomplete

**Severity: Medium**

The current gate covers Gitleaks, Semgrep OSS and Trivy filesystem scanning. Missing or experimental layers include:

- meaningful IaC enforcement;
- scan of the exact built image digest;
- SBOM and licence evaluation;
- provenance/signing;
- structured authenticated DAST;
- API security tests;
- Kubernetes admission/runtime controls;
- cross-file/dataflow SAST available only in more advanced engines.

Do not add breadth ahead of the trusted boundary and exception workflow. When expanding, clearly state tool limits and which evidence is merge-gating versus scheduled/advisory.

### M7 — Artifact build/promotion chain is not implemented

**Severity: Medium-high for release use**

Deployment versioning documentation now gives the platform a sensible vocabulary, but product repositories are not yet built once and promoted by digest with SBOM, vulnerability decision, provenance and signature verification.

Recommendation:

- build once from an authorised commit;
- generate CycloneDX/SPDX SBOM;
- scan the exact image/filesystem artifact;
- issue provenance containing source, gate and policy identities;
- sign artifact and attestations with independent keys;
- publish to a controlled registry and promote the same digest;
- verify signature/policy at deployment.

### M8 — Operational automation is documentation-heavy

**Severity: Medium**

`doctor.sh` and `quickstart.sh` are useful. `e2e.sh`, `backup.sh`, `restore-drill.sh` and `evidence-export.sh` do not exist. There are no Prometheus/Grafana/Loki services, central logs, dashboards, HA design, recovery objectives or automated upgrade/rollback rehearsal. Ansible has not run end-to-end on a supported POSIX controller.

Recommendation:

- implement backup and restore first, then evidence export and full e2e canary automation;
- run Ansible on a real Linux control node and correct discovered drift;
- add monitoring for gate latency, queue backlog, scanner/DB freshness, reconciliation, exceptions and runner capacity;
- define RPO/RTO and rehearse upgrades/rollback using pinned versions.

### M9 — Documentation contains stale implementation descriptions

**Severity: Medium-low**

`pipelines/fast.woodpecker.yml` still says differential gating is not implemented even though SADR-0024 and evaluator behaviour now implement it. `docs/what_next.md` also retains some pre-SADR-0024 narrative sections before later correcting them. In a security platform, contradictory descriptions can cause operators to configure the wrong trust model.

Recommendation:

- update pipeline comments to match current baseline behaviour;
- make `docs/TODO.md` the status authority and clearly mark historical critique sections;
- add a milestone-bound documentation reconciliation checklist;
- ensure architecture tables distinguish built, live-tested, deployed and required.

### M10 — Vendored-rule licensing is not formally approved

**Severity: Medium governance risk**

The OpenGrep-derived rules are pinned and their internal-use posture is documented. Formal licensing review has not been completed. Broad distribution/onboarding before approval may create legal and procurement risk.

Recommendation:

- retain source commit, licence texts and modification record;
- obtain written legal/procurement approval for intended internal and multi-team use;
- make update cadence, diff review and licence revalidation part of policy release.

## 8. Strengths to preserve

- Threat-driven SADRs grounded in reproduced failure modes.
- Live testing of status forgery, PR retargeting, stale approval and bot-vote freshness.
- Requirement for a current-head bot decision plus a distinct non-author human approval.
- Protected-base comparison for pipeline, evaluator, normalisers, severity policy, baseline and the full vendored-rule tree.
- Fail-closed handling of missing PR identity, truncated trees, invalid signatures and mismatched policy/contract identity.
- Unified finding schema and one reviewable Rego severity policy.
- Stable differential fingerprints and the rule that secrets are never baselined.
- Pinned scanner/platform versions and separate vulnerability DB refresh design.
- Private onboarding that keeps credentials out of clone URLs and `.git/config`.
- Reconciliation based on genuine PR events rather than unsafe synthetic status repair.
- Clear distinction between local tests, live pilot evidence and undeployed designs.
- Careful comments recording operational discoveries and why tempting alternatives were rejected.

## 9. Recommended execution order

### Immediate code-controlled fixes

1. Make baseline refresh fail closed on corrupt/unreadable existing baselines.
2. Validate and hash required scanner reports before the trusted bundle writes `result.json`.
3. Pass and bind the protected-base baseline explicitly in trusted-bundle evaluation.
4. Reconcile stale pipeline/TODO/what-next descriptions.
5. Add tests for every new failure path above.

### Trusted pilot

1. Establish a dedicated project repository and authoritative remote.
2. Provision the isolated trusted-runner boundary.
3. Replace or tightly bound HMAC with an independently managed signing identity.
4. Pin bundle, policy, contract and signer identities.
5. Enable required attestation for one canary repository.
6. Prove clean, failing, tampered, missing, stale and scanner-outage cases.
7. Retain signed evidence and document rollback.

### Usable enforcement

1. Implement false-positive suppression and two-party exceptions with expiry.
2. Install and benchmark chained pre-receive secret scanning.
3. Convert approval/reconciliation into a supervised multi-repository service.
4. Register Trivy refresh and evidence freshness alerts.
5. Add persistent evidence and export.

### Trusted release and operations

1. Build/SBOM/scan/provenance/sign/publish/promote by digest.
2. Encrypt platform secrets and narrow automation identities.
3. Add metrics, logs, dashboards and canary automation.
4. Implement backup/restore and upgrade/rollback rehearsals.
5. Complete licensing and framework-governance approvals.

## 10. Acceptance criteria for the next immediate fix

The baseline/report-integrity tranche should be considered complete only when:

- first-time baseline creation requires a demonstrably absent baseline or an explicit operator flag;
- malformed, unreadable, wrong-schema and wrong-repository baselines fail without overwriting anything;
- re-onboarding can only preserve or remove existing baseline fingerprints;
- all required scanner reports are present, structurally parsed and hashed before a result is written;
- a scanner exit 0 with no/empty/stale/malformed output produces no attestation;
- the trusted evaluator receives the protected-base baseline explicitly;
- fast and trusted decisions match for inherited, introduced, fixed and reintroduced findings;
- unit syntax/policy/normaliser/evaluator/bot/attestation/bundle tests pass;
- live canary proof records exact base/head SHA, bundle/policy/baseline/report digests and decision.

## 11. Conclusion

The SSDLC Platform Lab is strongest where many security projects are weakest: it actively attacks its own trust assumptions and records what was genuinely proven. The recent baseline, vendored-rule and Gitleaks fixes show that this approach finds real defects that mocked tests miss.

The platform should not broaden to more scanners or a polished portal yet. First close the two new baseline/report-integrity issues, move the merge-authorising decision onto an isolated trusted runner, and create an exception/evidence path that can survive real delivery pressure. Until then, describe the system accurately as a strong enforcement pilot with transitional controls—not as a production trust service.
