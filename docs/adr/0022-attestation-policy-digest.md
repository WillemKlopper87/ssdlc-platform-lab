# SADR-0022: Bind the trusted-runner attestation to the actual scanning policy

**Status:** Accepted, implemented.

**Date:** 2026-08-27
**Milestone:** 3 -- trusted pilot foundation
**Security & Privacy Impact:** High (integrity of the durable trust boundary SADR-0017 designed)

## Question

A 2026-08-27 review of the repo (SADR-0019/0020/0021's changes) flagged that
`gate-bundle/`'s trusted-runner bundle had silently drifted from the fast
gate's own policy: it shipped only two hand-written pilot Semgrep rules while
`pipelines/fast.woodpecker.yml` had moved to the 594-file vendored ruleset
(SADR-0020). A follow-up fix aligned the bundle's *scanner invocation* to use
`policy/vendored-rules` too -- but that alone does not close the gap
`gate-contract/contract.json`'s digest mechanism was supposed to close.
`contract_digest()` hashes `required_scanners` and the block-severity
thresholds -- it says nothing about which *rules* those scanners ran with. A
bundle rebuilt with a weakened or different ruleset produces an identical
`contract_digest`, and would attest and pass verification exactly as if
nothing had changed.

## Decision

Bind the signed attestation to a content-based digest of the exact scanning
policy that produced it, not just the contract's static shape.

**`gate_contract/attestation.py` gains `policy_digest(directory)`** --
deterministic SHA-256 over every file's `(relative_path, content_hash)` under
a directory, sorted. Content-based, not mtime/size-based, so a byte-identical
rebuild always reproduces the same digest and any single-byte rule change
anywhere in a 594-file tree changes it.

**`gate-bundle/run-pilot-bundle.py` computes this over its own bundled
`/opt/ssdlc/policy/`** (the same tree `gate-bundle/Dockerfile`'s
`COPY policy/ ./policy/` puts into the image) at scan time, and includes it
in `result.json` as `policy_digest`. This is the bundle attesting to itself:
whatever ruleset actually ran is what gets hashed, not a value trusted from
outside.

**`scripts/trusted-gate-runner.py`'s `issue()`** now requires `policy_digest`
to be present in the bundle's result (fails closed otherwise, matching how it
already treats a missing scanner result) and carries it into the signed
attestation document.

**`bot-approver.py`'s `trusted_attestation_matches`** requires a new
operator-configured `GATE_POLICY_DIGEST` (alongside the existing
`GATE_ATTESTATIONS_DIR`, `GATE_ATTESTATION_KEY`, `GATE_CONTRACT_DIGEST`) and
checks `attestation["policy_digest"] == GATE_POLICY_DIGEST`. Same pattern as
the existing contract-digest check: an operator-set expected value, not
something derived automatically -- the whole point is that the *bot* decides
what policy it trusts, not whatever the bundle happens to compute this run.

**`scripts/print-policy-digest.py`** computes the expected value from the
*source* `policy/` tree (the same content the Dockerfile copies), for an
operator to set as `GATE_POLICY_DIGEST` after reviewing a bundle release --
mirrors `print-gate-contract-digest.py`'s existing role for the contract
digest.

## A real bug found live, not caught by mocked unit tests

Rebuilding the actual bundle image to verify this end to end failed
immediately: `ModuleNotFoundError: No module named 'gate_contract'`.
`gate-bundle/Dockerfile` had never copied the `gate_contract/` module into
the image at all -- every existing unit test imports it directly from the
repo root via Python's normal import path, so this gap was invisible to
`tests/unit/test_gate_bundle.py` regardless of how thorough that test was.
Fixed: `COPY gate_contract/ ./gate_contract/` added to the Dockerfile.
Confirmed live after the fix: a rebuilt image, run directly (`--entrypoint sh`
override, matching the pattern needed throughout this session for ad-hoc
container testing), produced a `result.json` with a real, non-placeholder
`policy_digest` for a real scanned file.

## Open finding, not yet resolved

The same live verification run surfaced something separate and unresolved:
Gitleaks' own `gitleaks detect --source <workspace>` step produced **zero**
findings for a file containing an obvious AWS access key, in a workspace with
no `.git` directory (an ad-hoc test directory, not `trusted-gate-runner.py`'s
real `checkout_head`, which extracts a genuine git archive with real history).
The vendored Semgrep secrets rules still caught the same key, but classified
as INFO/low rather than gitleaks' own unconditional-critical mapping
(`normalise/gitleaks_adapter.py`). Docker crashed before this could be
isolated further (git-context artifact of the throwaway test vs. a real gap
in this specific `gitleaks detect` invocation). Tracked in `docs/TODO.md`,
not silently assumed benign -- re-check against a real `checkout_head`-style
workspace before trusting this bundle's secrets detection in production.

## Verification

- Unit: `test_gate_bundle.py` (digest computation is deterministic and
  content-sensitive; `result.json` includes it), `test_trusted_gate_runner.py`
  (missing `policy_digest` is not attested; a valid one is carried through
  and signed), `test_gate_attestation.py` (a wrong or missing
  `GATE_POLICY_DIGEST`/`policy_digest` fails closed). Full `tests/unit/` tier
  re-run clean.
- Live: rebuilt `gate-bundle/Dockerfile` end to end, ran the built image
  directly against a real scanned file, confirmed `result.json` contains a
  correctly-computed `policy_digest` and that the bundle otherwise still
  functions (Semgrep against the vendored rules, Trivy, policy evaluation all
  ran and produced a real pass/fail decision).

## Consequences

- Closes the specific gap the 2026-08-27 review named: a rebuilt bundle with
  a silently different ruleset can no longer pass attestation undetected.
- `GATE_POLICY_DIGEST` must be re-derived and updated on the bot sidecar
  every time the bundle is rebuilt with a rule change -- documented in
  `docs/OPERATIONS.md` and `gate-bundle/README.md`; an operator who forgets
  this gets every attestation failing closed, not a silent bypass, which is
  the correct failure direction.
- Still requires the isolated runner host to actually deploy and exercise
  this live end to end through `trusted-gate-runner.py` itself, not just the
  bundle in isolation -- same gap SADR-0017/0020 already named.
