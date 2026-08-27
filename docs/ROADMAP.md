# Product roadmap — secure development learning platform

## Purpose

This roadmap turns the SSDLC framework into a learning-first platform for
newer developers. The platform must make secure work understandable,
automate high-confidence checks, retain useful evidence, and only then grow
into stronger release and runtime enforcement.

This is a roadmap, not a claim that the controls below are already operating
or sufficient for ISO/IEC 27001 certification. Automation supplies evidence
for processes, risk decisions, and reviews; it does not replace them.

The current standards and learning-first principles that refine this roadmap
are recorded in [`FRAMEWORK-ADDENDUM-2026.md`](FRAMEWORK-ADDENDUM-2026.md).

## Product principles

1. Explain every finding: what happened, why it matters, and the safe next action.
2. Start with a small number of high-signal controls.
3. The code under review must not define, bypass, or approve its own gate.
4. Progressively enforce: learn, warn, block secrets/Critical, then tuned High.
5. Make exceptions specific, approved, time-limited, and retained as evidence.

## Roadmap

| Phase | Framework mapping | Outcome | Main work | Exit criteria |
|---|---|---|---|---|
| 0. Trusted pilot foundation | Governance / CI security | Safe boundary for one pilot repo | Approval freshness, trusted gate contract, pilot measures | A developer cannot bypass by altering policy, pipeline, status, or approvals |
| 1. Learn and guide | Design + development | Helpful feedback for new developers | Repo contract, pre-commit, PR comments, runbooks, training | Pilot team can remediate common findings without platform-admin help |
| 2. Measured enforcement | CI/CD | Reliable PR gating | Baseline, severity policy, exceptions, Renovate, metrics | Secrets and new Critical findings block; High follows measured tuning |
| 3. Trusted release | Deployment | Verifiable release artifacts | SBOM, provenance, Cosign, release approval | Release traces from protected source to signed artifact and deploy |
| 4. Evidence and operations | Maintenance / ISO evidence | Auditable, supportable platform | Evidence export, retention, KPIs, recovery drills | Sample PR, exception, release, and recovery evidence can be produced |
| 5. Runtime and scale | Runtime security | Controls for real platform targets | Runner separation, secrets manager, Kubernetes controls | Only attested images reach a real target; alerts/escalation are proven |

## Phase 0 — trusted pilot foundation

Do not onboard a real developer team until this phase is complete.

- Create a Gate Contract: required scanners, expected steps, policy digest,
  source SHA, runner identity, and approval rules.
- Run gate code and policy from a platform-controlled versioned artifact, not
  the PR checkout.
- Require the bot to validate an attestation for the exact PR head SHA and
  approved Gate Contract before approving.
- Explicitly dismiss stale approvals on a new commit and re-check retargets.
- Require one bot verdict and one non-author human approval independently; a
  count of two approvals alone is not enough.
- Add bypass regression tests for altered pipeline/policy, status forgery,
  stale approvals, retargeting, and missing attestations.

## Phase 1 — learn and guide

- Paved-road repository contract: owner, data classification, deployment type,
  language, dependencies, and risk tier.
- `pre-commit` with Gitleaks and language-appropriate fast checks. Local
  checks are coaching; CI remains authoritative.
- Keep Semgrep, Gitleaks, and Trivy as the initial CI set. Inline comments
  should explain remediation and link to an internal example.
- Create `SECURITY.md`, `DEVELOPER_SETUP.md`, and a failed-gate runbook.
- Hold a weekly security clinic; use recurring confusion to improve templates.

## Phase 2 — measured enforcement

- Review a pilot baseline first; do not initially block inherited debt.
- Enforce in stages: secrets, then new Critical, then High after measurement.
- Build two-person exceptions bound to finding fingerprint, repository,
  PR/commit, expiry, and approvers.
- Deploy Renovate with a Gitea Dependency Dashboard and agreed schedule.
- Publish a small dashboard: gate state, new findings, aging exceptions,
  dependency state, and remediation trend.

## Phase 3 — trusted release

- Generate CycloneDX or SPDX SBOMs with the already-pinned Syft.
- Emit in-toto/SLSA-style provenance containing Gate Contract and source SHA.
- Sign artifacts, SBOMs, and attestations with Cosign; verify before release.
- Add IaC/container scans and release approval only for a real target.

## Phase 4 — evidence and operations

- Retain immutable references to PR decisions, attestations, approvals,
  exceptions, SBOMs, signatures, and deployment verification.
- Add evidence export, retention, owner review cadence, and KPIs: false
  positives, remediation time, exception age, dependency age, escaped findings.
- Exercise backup/restore and an incident-response game day.

## Phase 5 — deliberately postponed work

- DefectDojo and Dependency-Track for central findings/SBOM management.
- OpenBao (or equivalent) for runtime secrets; keep sops + age for encrypted
  Git-managed configuration.
- Dedicated untrusted, gate, integration, and release runners; untrusted jobs
  never receive Docker socket or release secrets.
- Kubernetes: Kyverno, Sigstore Policy Controller, Falco, WAF, SIEM, and image
  attestation admission.
- OpenSSF Scorecard and Security Insights for a public mirror or supported forge.
- Custom developer portal; start with Gitea PR feedback and a small dashboard.
