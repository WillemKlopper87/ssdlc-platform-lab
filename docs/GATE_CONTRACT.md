# Gate Contract

The invariants the platform's merge decision depends on, and how each is actually tested today.
Origin: [SADR-0017](adr/0017-trusted-gate-contract.md), written for Sprint 01's core question —
*"how can the platform establish that a gate verdict was produced by the intended policy and scanner
configuration, rather than by code supplied by the pull request the gate is deciding whether to
merge?"*

This document is the checklist. `docs/adr/0017-trusted-gate-contract.md` is the reasoning. Read the
SADR before changing any invariant here — the reasoning behind *why* each one exists is not repeated
in full below.

## The contract, as data

`gate-contract/contract.json` is the platform-owned description of the pilot gate:

```json
{
  "schema_version": 1,
  "gate_release": "pilot-2026-08-26",
  "required_scanners": ["secrets", "sast", "dependencies"],
  "decision": {
    "block_severities": ["critical", "high"],
    "fail_closed_on_missing_report": true
  }
}
```

`scripts/print-gate-contract-digest.py` prints its canonical SHA-256 digest — the value a trusted
runner and the bot compare to detect a contract change out from under them.

## Invariants and how each is tested

| Invariant | Mechanism | Tested by |
|---|---|---|
| A PR cannot forge the gate's commit status | Status context is set by Woodpecker's own native Gitea integration (D1), not by anything a developer's token can write with the same trust weight | Live: [SADR-0001](adr/0001-commit-status-forgery.md) confirmed the forgery, then confirmed the fix (identity-bound approval, not status alone) closes it |
| A count of approvals is not proof of the right approvers | `verify-approvals.py` requires the configured gate bot's *current-head* approval plus a distinct *non-author human* approval when `GATE_BOT_LOGIN` is set — not merely `required_approvals: 2` | Unit: `tests/unit/test_policy_eval.py` scenarios 6-11. Live: [SADR-0011](adr/0011-bot-approver.md), [SADR-0013](adr/0013-team-based-approval-whitelist.md) |
| An approval does not survive a retarget to a different base | `verify-approvals.py` reads PR timeline `change_target_branch` events and rejects any approval submitted at or before the last retarget | Live: [SADR-0003](adr/0003-pr-retargeting-approval-bypass.md) confirmed the bypass (CVE-2026-58439) and that `dismiss_stale_approvals` alone does *not* close it; `verify-approvals.py`'s independent re-derivation does |
| An approval does not survive a new commit | Branch protection's `dismiss_stale_approvals: true`, set by `onboard-repo.sh` | Live, part of SADR-0011's end-to-end merge test |
| A PR cannot redefine the pipeline, evaluator, adapters, or policy that approves it | `bot-approver.py`'s `gate_contract_matches_base` byte-compares every `GATE_MANAGED_PATHS` file at PR head against the protected base via Gitea blob SHAs, before it will cast the bot's vote | Live-tested per SADR-0017's "current implementation status"; consolidated regression coverage for altered-pipeline/altered-policy attempts is listed in `docs/TODO.md` as still outstanding |
| A gate verdict is bound to the exact PR head SHA, not a prior or later one | `verify-approvals.py` checks `review.commit_id == head_sha`; `bot-approver.py` checks the bot's own latest review's `commit_id` before skipping; `trusted_attestation_matches` checks `head_sha` against the attestation filename and body | Unit: `test_policy_eval.py` scenario 10. Live: SADR-0009, SADR-0011 |
| A missing or malformed scanner report fails closed, never silently passes | `evaluate-findings.py` exits 2 on unreadable/malformed input; `verify-approvals.py` exits 2 on API failure | Unit: `tests/unit/test_evaluate_findings.py` ("a Conftest crash fails closed even if it prints JSON", "malformed scanner JSON fails closed") |
| A lost webhook does not leave a PR stuck forever with no verdict | `scripts/reconciliation-loop.py` -- pushes a real empty commit through the real webhook path (deliberately not a synthesized event; see the SADR for two rejected alternatives that would have reintroduced a bypass) | Live: [SADR-0015](adr/0015-reconciliation-loop.md), including a runaway-safety limit (max 3 nudges) |
| A trusted-runner attestation is bound to this repo, PR, head SHA, and contract digest, and cannot be forged without the key | `gate_contract/attestation.py`'s HMAC-SHA256 signing over canonical JSON; `trusted_attestation_matches` checks identity, digest, decision, and required-scanner success before trusting it | Not yet live -- no isolated runner host exists in this environment to deploy `trusted-gate-runner.py` against. Unit-testable today (`tests/unit/test_gate_attestation.py`, `test_trusted_gate_runner.py`, `test_gate_bundle.py`) |

## Two enforcement modes, and where each stands

**`GATE_CONTRACT_ENFORCE=1` (Sprint 01 transitional control, enabled by default).**
`bot-approver.py` trusts Woodpecker's own pipeline result (the same untrusted-checkout pipeline
described in [`ARCHITECTURE.md`](ARCHITECTURE.md)'s trust-boundary section), but only after
confirming every gate-managed file is byte-identical to the protected base. This is real protection
against the specific attack SADR-0017 names -- a PR silently weakening its own evaluator -- but it is
**not** an attestation. It proves the gate files weren't changed; it does not prove the gate ran in an
isolated, tamper-proof environment. The scanning steps still execute inside the same Woodpecker agent
that runs the PR's own build.

**`GATE_ATTESTATION_REQUIRED=1` (the durable design, built, not deployed).**
The bot ignores Woodpecker pipeline results entirely and requires a signed attestation from
`scripts/trusted-gate-runner.py`, which:
1. Downloads the exact PR head via a **read-only** token, outside Woodpecker.
2. Extracts it to a fresh directory and invokes `gate-bundle/run-container.sh` -- an operator-owned
   wrapper that refuses anything but an immutable `@sha256:` image digest, and runs it with
   `--network none --read-only --cap-drop ALL`, workspace mounted read-only, no Docker socket, no
   forge token, no bot credential inside the container.
3. Validates the bundle's `result.json` (decision + per-scanner success) against the loaded contract's
   `required_scanners`.
4. Signs and writes the attestation atomically (`gate_contract/attestation.py`, `os.replace` to avoid
   a torn read).

`bot-approver.py` then checks: signature valid, repository/PR/head-SHA/contract-digest/decision all
match, and every required scanner reports `success` -- failing closed on any missing configuration,
missing file, bad signature, or mismatch.

**Turning this on requires, in order:** an isolated runner host (this environment has only one
machine -- the same honest gap SADR-0010 recorded for Ansible and SADR-0016 recorded for Tier 2 of the
regression suite), a normal and a deliberately-altered PR both tested against it, then setting
`GATE_ATTESTATION_REQUIRED=1` and `GATE_CONTRACT_DIGEST` on the bot per `docs/OPERATIONS.md`'s
"Trusted-runner pilot" section.

## The HMAC key is a pilot bridge, not a production signing design

`gate_contract/attestation.py`'s own docstring says this plainly: HMAC-SHA256 with a shared secret is
easy to operate locally, and the durable design in SADR-0017 replaces it with a separately managed
signing identity and verified release provenance (Cosign, in-toto/SLSA-style attestations -- see
DESIGN.md's supply-chain integrations). Do not treat `GATE_ATTESTATION_KEY` as a long-term credential;
it is a stand-in for a real signing key, scoped to the runner and the bot sidecar only, never a build
container.

## What a PR can and cannot influence

A PR **can**: change application code, add or fix findings, choose what gets scanned (by changing the
code itself).

A PR **cannot** (once `GATE_CONTRACT_ENFORCE=1` or `GATE_ATTESTATION_REQUIRED=1` holds): change which
scanners run, what severities block, how approvals are counted, what counts as the current head, or
which policy digest is trusted -- without the bot refusing to vote. This is the concrete answer to
SADR-0017's question: not "did some pipeline exit 0," but "did the intended, unaltered gate produce
this verdict for this exact commit."

## Outstanding, tracked honestly (see `docs/TODO.md`)

- Regression cases for altered-pipeline, altered-policy, missing-report, forged-status, and
  old-bot-approval attempts -- some proven live piecemeal across earlier SADRs, not yet consolidated
  into one bypass-test suite specific to the Gate Contract.
- `trusted-gate-runner.py` deployment against a real isolated host.
- An independently managed signing identity to replace the HMAC bridge.
