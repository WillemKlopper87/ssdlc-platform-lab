# SADR-0019: Gate Contract bypass regression suite, live-tested

**Status:** Accepted, implemented.

**Date:** 2026-08-27
**Milestone:** 3 — trusted pilot foundation, Sprint 01
**Security & Privacy Impact:** High

## Question

SADR-0017 established the Sprint 01 transitional control — `bot-approver.py`'s
`GATE_CONTRACT_ENFORCE` byte-comparison of every gate-managed file against
the PR's protected base — and its own "current implementation status"
section noted it as "live-tested per SADR-0017," but `docs/TODO.md` still
listed consolidated bypass regression coverage (altered pipeline, altered
policy, stale approval, old bot approval) as outstanding. Every other
security-critical mechanism in this project pairs a unit test with a live
regression test before being trusted (SADR-0001 through SADR-0015 all
follow this pattern); the Gate Contract's file-comparison logic had only
`tests/unit/test_bot_approver.py`'s mocked-`file_at_ref` coverage. Does the
mechanism actually hold against a real Gitea+Woodpecker stack, or only
against a mock of it?

## Decision

Add `tests/regression/07-gate-contract-bypass.sh`, wired into
`run-minimal-suite.sh` immediately after `05-bot-approver.sh` (reuses its
Woodpecker polling helpers). Three scenarios, twelve assertions:

**A — altered gate file.** A PR that changes `.woodpecker.yml` relative to
the protected base, but keeps its own pipeline reporting green (the
realistic shape of this attack: a contributor weakens `approval-check` or a
scanning step while keeping the pipeline's *overall* status green), gets
**zero** bot votes — confirmed the PR's own pipeline really did report
`success` on `code-scan` first, so the rejection is provably the
file-comparison firing, not an unrelated pipeline failure masking the
result. The bot's own log names the specific altered file.

**B — stale human approval.** An approval survives neither Gitea's own
`dismiss_stale_approvals` nor — independently, per SADR-0003's lesson that
Gitea's own state cannot be trusted alone — `verify-approvals.py`'s own
`commit_id` check, once a new commit supersedes the approved head.

**C — stale bot approval, and correct recovery.** The identical freshness
requirement applied to the bot's own vote: an old bot approval for a
superseded head fails `verify-approvals.py`'s `GATE_BOT_LOGIN`-specific
branch with its own named reason ("required gate bot ... has not approved
the current head"), not a generic approval-count failure — proving that
branch is exercised, not merely present in the source. Critically, the bot
does **not** get stuck refusing to ever vote again: once the new head's own
pipeline reports green, `bot-approver.py` re-votes for it (its own
`commit_id`-mismatch skip-check correctly does not treat the stale vote as
"already handled"). A normal `git push` to an open PR's branch was
confirmed to produce a fresh `pull_request`-event pipeline on its own — no
reconciliation-loop intervention needed for this path, distinct from
SADR-0015's scenario of a PR with no webhook history at all.

"Missing report" and "forged status" were judged already adequately
covered elsewhere and were not duplicated: the former by
`test_evaluate_findings.py` and `test_gate_attestation.py`'s existing
fail-closed unit coverage, the latter live in SADR-0001 itself.

## Two real bugs found building this, not just written and trusted

**`tests/regression/lib.sh`'s `woodpecker_get_pat` only handled first-time
OAuth consent.** Against an account that had already authorized the
Woodpecker OAuth app in a prior session, Gitea skips the consent HTML page
entirely and 303-redirects straight to Woodpecker's callback with the code
already attached. The original function unconditionally tried to scrape
`state`/`redirect_uri` from a consent form that, in this case, never
existed — silently extracting empty values, POSTing a no-op grant, and
returning the opaque string `"User not authorized"` (19 characters) in
place of a real token, with nothing to indicate why. Every prior live test
of this helper happened to run against a freshly-provisioned, never-before-
authorized account (a fresh `docker compose down -v` harness each time),
so this path was never exercised until this session's debugging needed to
run against this environment's own long-lived, already-authorized
`gateadmin` account. Fixed: detect which case actually happened from the
grant-page fetch's own response (a `Location` header present means
already-authorized; its absence means a real consent form was returned)
rather than assuming.

**A nested command substitution for Woodpecker repo activation silently
swallowed a failure mode.** The first draft computed the Gitea repo id and
fed it directly into the Woodpecker activation call's query string via a
nested `$(...)`, with the whole thing wrapped in one more `$(...)` to
extract the activation response's own `id` field. When Woodpecker returned
an already-activated response for a stale record (harmless in the real
onboarding flow, which treats a `409` as "already active, continuing" —
see `onboard-repo.sh` — but this test suite's debug iterations against a
persistent, previously-poked-at stack hit it repeatedly), the failure
propagated as an opaque downstream JSON error far from its actual cause.
Rewritten to the same explicit, step-by-step pattern `05-bot-approver.sh`
already uses successfully (`repo_id=...`; `wp_repo=...`; `wp_repo_id=...`
as three separate statements) — proven, and far easier to debug when it
does fail.

Also found, live, while debugging: Woodpecker 3.17.0's `GET
/api/user/repos` listing response and its `POST /api/repos?forge_remote_id=`
activation response use genuinely different JSON schemas — the listing
uses `forge_remote_id` (a string) and has no numeric `id` field at all,
while the activation response does. Confirmed directly via raw response
inspection rather than assumed from either endpoint's shape alone; worth
knowing for anyone next touching Woodpecker's repo API in this project.

## Consequences

- The Sprint 01 transitional control (`GATE_CONTRACT_ENFORCE`) and the
  approval-freshness mechanism (`verify-approvals.py`'s bot/human,
  current-head checks) are now both live-regression-tested, not merely
  unit-tested — closing the specific gap `docs/TODO.md` named.
- `woodpecker_get_pat`'s fix benefits every future regression run against
  any already-authorized account, not just this test — a latent bug that
  would otherwise have resurfaced opaquely the next time anyone ran this
  suite against a long-lived stack instead of a fresh one.
- Still open, unchanged by this work: the durable trust boundary
  (`trusted-gate-runner.py` deployed against a genuinely isolated host) —
  this regression suite proves the *transitional* control holds, not that
  the durable design is deployed. See `docs/GATE_CONTRACT.md`.

## Verification plan (completed)

- `sh -n` syntax check on the new file and the modified `lib.sh`.
- Live run against `compose/minimal` (the persistent stack from this
  session, not a fresh `run-minimal-suite.sh` harness, to avoid destroying
  unrelated pilot-app history built up earlier in the same session):
  12/12 assertions passed, including the positive-path remediation
  (scenario B/C's final fresh-approval PASS), proving the suite can
  distinguish a real bypass from a real fix rather than always failing
  closed regardless of input.
- Full `tests/unit/` tier and both lint scripts re-run clean after the
  `lib.sh` change.
