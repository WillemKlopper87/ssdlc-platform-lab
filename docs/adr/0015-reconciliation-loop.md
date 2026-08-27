# SADR-0015: The general reconciliation loop — a real stuck PR, detected and recovered without a synthesized webhook

**Status:** Confirmed by experiment: detection, safe recovery, idempotency, and the runaway-safety
limit all live-tested against a genuinely stuck PR
**Date:** 2026-08-26
**Milestone:** 3/4 (Enforcement) — the general case D4 specified, beyond what SADR-0011's
bot-approver already covers for its own one path
**Security & Privacy Impact:** Medium. This sits in the *availability* path, not the *integrity*
path (D4's own framing) — it cannot make a red PR green, only un-stick a PR that never got a verdict
at all. The two mechanisms considered and rejected while designing it, however, are a direct
integrity concern, and are the real substance of this SADR.

## Question

D4 named this precisely: Gitea does not auto-retry a lost webhook delivery, and `docs/TODO.md`
already flagged that SADR-0011's `bot-approver.py` only closes this for the one path where the bot's
*own* vote needs a fresh evaluation — "a human's approval still triggers nothing," and more broadly,
any lost webhook for any reason leaves a PR stuck forever with no verdict. Does a general
reconciliation loop exist that closes this without opening a worse hole than the one it closes?

## Method — the two mechanisms that looked obvious and were both wrong

Checked Woodpecker's and Gitea's own source before writing anything, exactly the discipline that
already avoided the earlier `getaddrinfo`-class mistakes this session:

**Candidate 1 — Woodpecker's `POST /api/repos/{id}/pipelines` (`CreatePipeline`).** Read
`server/api/pipeline.go` directly: it calls `_forge.BranchHead(...)` and creates a pipeline with
`model.EventManual` — not `model.EventPullRequest`. `fast.woodpecker.yml`'s `approval-check` step is
scoped `when: event: pull_request`. A manually-triggered pipeline would simply **skip** that step —
and a reconciliation script that treated "a pipeline ran" as "the PR is fine" would be wrong in
exactly the way that matters: it would report a gate verdict for a PR whose approvals were never
re-checked at all.

**Candidate 2 — Gitea's `POST /repos/{o}/{r}/hooks/{id}/tests` (`TestHook`).** Read
`routers/api/v1/repo/hook.go` directly: its own swagger summary is "Test a **push** webhook." It
synthesizes a push-shaped payload for a given ref, never a `pull_request`-shaped one. Worse than
candidate 1 in one respect: `fast.woodpecker.yml`'s scanning steps (`secrets`/`sast`/`dependencies`)
**do** match `event: [push, pull_request]`, so they'd actually run and could report `success` — on
a status context (`ssdlc/security-gate/push/woodpecker`) that branch protection's glob pattern
(`ssdlc/security-gate/**`) is configured to accept as satisfying the required check. That is not a
theoretical concern; it is the exact shape of bypass SADR-0001, SADR-0003, and SADR-0009 already
spent real effort closing, reintroduced by the "fix" for a different problem.

**What's actually safe:** a real `git commit --allow-empty`, pushed for real, through the same path
a developer's own push takes. Gitea fires its own genuine push and PR-synchronize webhooks for it —
nothing synthesized, nothing this script has to vouch for the shape of. `scripts/reconciliation-loop.py`
does only this.

Detection is scoped narrower than D4's literal wording on purpose: a PR counts as stuck only if **no**
status context matching `ssdlc/security-gate/pr/*` exists at all for its current head SHA — not
"non-terminal." Distinguishing a genuinely-lost webhook from a pipeline that is simply still running
needs a staleness threshold this pass doesn't build (tracked as a follow-up, not silently assumed
solved).

## Result — live-tested against a genuinely, deterministically stuck PR

Simulated the real scenario D4 describes (a lost webhook), not a synthetic stand-in for it: pushed a
feature branch and opened a PR **before** activating the repo in Woodpecker, so Gitea genuinely had
no webhook registered at all — confirmed via `GET .../commits/{sha}/status` returning `total_count: 0`
before proceeding. Activated the repo afterward (registers a webhook for *future* events only,
proving activation doesn't retroactively fix anything) and confirmed the PR was still stuck.

```
run reconciliation-loop.py (RUN_ONCE=1):
  PR #1: no 'ssdlc/security-gate/pr/*' status for head 617e163759d4 -- nudging
  [feature/change 3a8b929] reconciliation: no gate verdict was ever recorded ...

GET .../commits/3a8b929.../status
  combined state: success
    ssdlc/security-gate/push/woodpecker -> success
    ssdlc/security-gate/pr/woodpecker   -> success   <- a REAL pull_request pipeline ran

re-run reconciliation-loop.py -- no output, no nudge (correctly idempotent: the current
  head SHA now has a real status, nothing to recover)
```

**Confirmed.** The recovered SHA carries a genuine `.../pr/...` status because a genuine
`pull_request` webhook fired for it — not a synthesized stand-in that would have skipped
`approval-check` or (worse) satisfied the glob on a push-shaped status alone.

**The runaway-safety limit, also live-tested, not just written and trusted:** deactivated the repo
again (simulating a *persistently* broken delivery path, not a one-off lost event) and ran five
simulated poll cycles against a second PR in the same process:

```
cycle 1: nudging (1st empty commit)
cycle 2: nudging (2nd empty commit)
cycle 3: nudging (3rd empty commit)
cycle 4: "still no gate status after 3 nudges -- stopping, this needs a human, not another automated push"
cycle 5: same -- no further action
```

Confirms the script does not spam commits indefinitely against infrastructure that reconciliation
itself cannot fix — exactly the failure mode a naive "just keep retrying" implementation would have
hit.

One real bug found live along the way: Gitea returns `"statuses": null` (a **present** key with a
`null` value) when none exist yet, not an absent key — Python's `dict.get("statuses", [])` only
supplies its default for a missing key, so the script crashed on its very first real run
(`TypeError: 'NoneType' object is not iterable`) until fixed to `status.get("statuses") or []`.

## Decision

1. **`scripts/reconciliation-loop.py` closes the general case** `docs/TODO.md` flagged as only
   partially covered by `bot-approver.py`'s own narrow restart path. A human's approval, a platform
   restart that lost in-flight webhooks, or any other delivery failure now gets recovered the same
   way, not just the bot's own vote.
2. **Never build a "re-trigger" mechanism on top of a synthesized event**, in this codebase or any
   future one that touches this gate. Both candidates rejected here looked like the obvious API to
   reach for; both would have silently reintroduced a bypass this project already closed once. Real
   git operations through the real webhook path are the only trustworthy recovery primitive.
3. **A new, third narrowly-scoped credential**: `GITEA_RECONCILE_TOKEN`, `write:repository` only, on
   its own identity (`reconcile-bot` in this experiment) — distinct from `policy-eval`'s read-only
   token and the gate bot's approve/merge token. Three jobs, three tokens, matching this project's
   now-consistent pattern of scoping each automated identity to exactly what it needs.
4. **The `dismiss_stale_approvals` interaction is a real, documented side effect, not a bug**: if a
   repo has it enabled, a reconciliation nudge dismisses existing approvals the same way any other
   developer push would. Correct, since the code that actually lands is the new commit and deserves
   the same fresh review — but worth remembering when explaining an unexpected "approval dismissed"
   to a confused developer.
5. **Staleness detection for a genuinely-stuck `pending` status (not just "no status at all") remains
   unbuilt**, honestly flagged rather than silently assumed covered by this pass.
6. **Multi-repo support and standing-service deployment are the same open items `bot-approver.py`
   already has** (`docs/TODO.md`) — this script has the identical single-repo, run-manually shape for
   now, and is a natural candidate to consolidate with `bot-approver.py` once a real sidecar exists
   to run both.

## Reproduce it yourself

```
cd compose/minimal
# bring up postgres, gitea (per docs/adr/0014's Terraform-first sequence)
# create gateadmin (admin), alice, reconcile-bot; mint tokens
# create a repo, add alice + reconcile-bot as write collaborators
# alice pushes a feature branch + .woodpecker.yml, opens a PR -- all BEFORE activating
#   the repo in Woodpecker, so no webhook exists yet
# confirm GET .../commits/{sha}/status -> total_count: 0
# NOW activate the repo in Woodpecker (registers the webhook going forward only)
# confirm still total_count: 0 for the original head SHA -- activation doesn't retroact
GITEA_URL=... GITEA_RECONCILE_TOKEN=... GITEA_RECONCILE_USER=reconcile-bot \
  REPO_OWNER=gateadmin REPO_NAME=<repo> RUN_ONCE=1 \
  python3 scripts/reconciliation-loop.py
# -> pushes a real empty commit; check the PR's NEW head sha for a genuine
#    ssdlc/security-gate/pr/* status
docker compose down -v && terraform destroy -auto-approve   # (in terraform/local/)
```
