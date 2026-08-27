# SADR-0017: Trusted Gate Contract

**Status:** Accepted for Sprint 01 implementation; full trusted-runner delivery remains open.

**Date:** 2026-08-26  
**Milestone:** 3 — trusted pilot foundation  
**Security & Privacy Impact:** Critical

## Question

How can the platform establish that a gate verdict was produced by the intended
policy and scanner configuration, rather than by code supplied by the pull
request that the gate is deciding whether to merge?

## Context

The original paved-road implementation copies `.woodpecker.yml`, policy
evaluation code, normalisation adapters, and Rego policy into each onboarded
repository. This is useful bootstrapping, but it is not a trustworthy final
boundary: a contributor can propose a change to those files in the same PR they
want approved. A successful pipeline status or two approvals alone cannot prove
that the intended gate ran.

SADR-0001 already established that a Gitea status context is forgeable by a
write collaborator. SADR-0011 added a bot approval, but its first version only
checked that pipeline steps appeared green. That is insufficient if the PR can
define the steps.

## Decision

Every automated approval must be bound to a **Gate Contract** for the exact PR
head SHA. The contract records:

- source repository, target branch, PR number, and exact head SHA;
- platform gate release/version and trusted policy-bundle digest;
- required scanner identifiers, pinned image digests, and required reports;
- normalized findings, decision, and execution timestamp;
- trusted runner identity; and
- gate-bot approval and distinct non-author human approval outcomes.

The bot must fail closed unless it can verify a current-head attestation that
satisfies the configured contract. It must not infer security from arbitrary
successful steps in a PR-controlled pipeline.

The durable architecture is a platform-controlled gate bundle/image, signed
and versioned outside the application repository. Policy is loaded from a
signed OPA bundle and result/provenance is signed with the platform identity.
The application checkout is scan input only; it is never the authority for gate
logic or release credentials.

## Sprint 01 transitional controls

Until the trusted runner is implemented, these controls are mandatory:

1. `verify-approvals.py` requires a configured gate-bot's approval for the
   current head plus a distinct non-author human approval.
2. Branch protection explicitly enables dismissal of stale approvals.
3. Regression coverage must prove that approvals on previous heads, stale
   retarget approvals, altered pipeline/policy, and forged statuses cannot
   cause a bot approval or merge.
4. No real repository may be treated as onboarded until the altered-pipeline
   proof exists. The existing per-repository pipeline remains a pilot-only
   transitional mechanism, not the final trust boundary.

## Current implementation status

The first three transitional controls have now started landing in the lab:

- branch onboarding sets `dismiss_stale_approvals`, and approval evaluation
  checks each review's exact current-head commit ID;
- approval evaluation requires the named gate bot and a different human,
  rather than accepting any two people; and
- before voting, `bot-approver.py` compares every managed gate file at the PR
  head with its protected base revision using Gitea blob SHAs. A missing or
  changed file is rejected without approval. This is enabled by default with
  `GATE_CONTRACT_ENFORCE=1`.

This is deliberately a transition, not an attestation. A protected-base
comparison prevents a contributor from altering gate logic in their own PR;
it does not make the copied files an independently released, signed gate
bundle. The durable decision above remains required before production use.

## Attestation bridge (implemented, not yet deployed)

`gate-contract/contract.json` is the platform-owned description of the pilot
gate. `scripts/print-gate-contract-digest.py` prints its canonical SHA-256
digest. A future trusted runner must execute the pinned bundle outside the
application CI context, then use `scripts/issue-gate-attestation.py` to place
a signed result in its private attestation directory. The result is bound to
the repository, pull request, exact head SHA, contract digest, pass decision,
and successful `secrets`, `sast`, and `dependencies` results.

Once that runner exists, configure its bot sidecar with:

```text
GATE_ATTESTATION_REQUIRED=1
GATE_ATTESTATIONS_DIR=<private shared directory>
GATE_ATTESTATION_KEY=<runner/bot-only secret>
GATE_CONTRACT_DIGEST=<output of print-gate-contract-digest.py>
```

The bot then fails closed before it considers Woodpecker steps if the file is
missing, tampered with, for another head, for another contract, non-passing,
or lacks a successful required scanner. `GATE_ATTESTATION_REQUIRED` remains
`0` during the migration because no runner service is deployed yet. The HMAC
key is a pilot bridge, not a production signing design: it must be replaced by
an independently managed signing identity and signature verification before
production use.

## Consequences

- The implementation is more work than adding another scanner, but it closes
  the authority gap at the centre of the platform.
- The gate bundle becomes a security-sensitive release artifact and needs its
  own review, version pin, signing, and rollback process.
- Developers retain simple feedback: they interact with PR comments and a
  status, not the policy bundle or platform credentials.
- Later SLSA/in-toto provenance, Cosign verification, and admission controls
  extend this same contract to released artifacts rather than creating a second
  security model.

## Verification plan

- Unit tests: bot/human role checks, current-head approval checks, malformed
  contract/report fail-closed behavior.
- Regression harness: status forgery, retargeting, stale approval after new
  commit, and altered pipeline/policy attempts.
- Live pilot: a normal PR can merge only after a valid contract attestation,
  bot approval, and human approval; an altered gate definition cannot obtain
  the bot approval.
