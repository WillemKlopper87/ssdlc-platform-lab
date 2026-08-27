# Sprint 01 — trusted pilot foundation

**Goal:** make one pilot repository safe enough to teach with, without
pretending the platform is production-ready.

**Suggested duration:** two weeks. Do not add more scanners, Kubernetes, or a
custom dashboard in this sprint.

## Sprint outcome

A developer cannot merge merely by changing the PR pipeline or policy,
forging status, relying on stale approval, or obtaining two ordinary human
approvals. The platform produces understandable feedback and a minimal
decision record for the pilot.

## Committed work

- [x] Write SADR: Trusted Gate Contract: immutable source, expected scanners,
      policy digest, head-SHA binding, attestation, and bot verification.
- [x] Correct branch protection: dismiss stale approvals and prove a human
      approval cannot survive a new commit.
- [x] Extend `verify-approvals.py` to require a current-head bot approval and
      distinct non-author human approval. Do not rely only on an approval count.
- [ ] Replace PR-controlled gate files with a platform-controlled gate bundle
      or image. At minimum, bot approval must require a pinned contract digest.
- [ ] Make `bot-approver.py` verify expected signed contract, scanner results,
      and current PR SHA; missing/altered data fails closed. (The verifier and
      attestation schema are now in place; deployment of the external runner
      and an independently managed signing identity remain open.)
- [ ] Add regression cases: altered pipeline, altered policy, missing report,
      forged status, stale approval, retarget, and old bot approval.
- [x] Add pilot developer guidance: `SECURITY.md`, setup, severity behaviour,
      and "what to do when a gate fails".
- [x] Add optional `pre-commit` with Gitleaks and language-neutral fast checks.

## Acceptance criteria

- [ ] Existing unit and regression tests pass.
- [ ] New bypass tests fail against the old implementation and pass after fix.
- [ ] A live run proves a modified PR pipeline cannot obtain bot approval/merge.
- [ ] A live run proves approval becomes invalid after a new commit and retarget.
- [ ] A valid pilot PR receives actionable feedback and merges only with current
      bot and human approvals.
- [ ] The pilot team can locate a finding, remediation guidance, and exception path.

## Explicitly out of scope

- DefectDojo, Dependency-Track, OpenBao, Kubernetes, admission control, Falco,
  WAF/SIEM, full evidence store, custom portal, and broad team rollout.
- Blocking Medium/Low or inherited legacy findings.

## Sprint review evidence

Keep test output, test repo/commit identifiers, the updated SADR, examples of
developer feedback, and remaining risks. This is useful ISO/IEC 27001-style
evidence: proof that a documented control was tested and reviewed, not a
certificate.
