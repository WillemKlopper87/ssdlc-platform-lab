# SADR-0009: `policy-eval` closes the PR-retargeting approval bypass — live-tested against the exact SADR-0003 attack

**Status:** Confirmed by experiment; the bypass is blocked, three additional cases live-tested to
guard against a shallow fix
**Date:** 2026-08-24
**Milestone:** 3 (Enforcement) — first concrete artifact of the `policy-eval` step named in D1
**Security & Privacy Impact:** Critical. Closes the second of the two live-confirmed, independent
approval-bypass holes tracked since SADR-0001 (status forgery — closed by `required_approvals: 2`
plus a bot-only approver team; the PR-retargeting bypass tracked here was the remaining unmitigated
one).

## Question

[SADR-0003](0003-pr-retargeting-approval-bypass.md) confirmed live that Gitea does not recalculate a
review's `official` flag when a PR's base branch changes, and that `dismiss_stale_approvals` does not
help — it watches for new commits, not a retarget. Its decision was explicit and specific:

> the gate's own `policy-eval` step (D1) must independently verify, at evaluation time, that every
> review counted toward `required_approvals` was submitted **against the PR's current base**, not
> merely that a review with `official: true` exists

This had not been built. `policy-eval/` was an empty directory. This experiment builds it — a real
script, not a design sketch — and live-tests it against the identical attack SADR-0003 used, plus
three further cases chosen to catch a fix that only happens to work on the one scenario already
demonstrated.

## Method

Reused the `compose/forgery-test` harness (Gitea `1.27.2`). Before writing any verification logic,
checked empirically what data Gitea actually exposes, rather than assuming a shape:

- `GET .../pulls/{n}/reviews` — confirmed fields `state`, `official`, `dismissed`, `submitted_at`,
  `commit_id`, `user.login` (already known from SADR-0003).
- `GET .../issues/{n}/timeline` — **not previously queried by this project.** Confirmed live that a
  retarget produces a `type: "change_target_branch"` timeline event carrying `old_ref`, `new_ref`,
  and `created_at` — exactly the signal needed to determine *when* a base last changed, independent
  of Gitea's own (broken) `official` recalculation.
- `GET .../branch_protections/{branch}` — confirmed live this requires the caller to hold **repo-admin
  permission**, not merely `write`: a plain write collaborator's token gets `403 "user should be an
  owner or a collaborator with admin write of a repository"`. This is a real operational constraint,
  not an assumption — it determines what kind of account `policy-eval`'s token must belong to.
- Confirmed a **minimally-scoped token** (`read:repository, read:issue` only, no write scopes) is
  sufficient for all four calls the script needs (`pulls/{n}`, `branch_protections/{branch}`,
  `issues/{n}/timeline`, `pulls/{n}/reviews`), provided it belongs to an admin-permission account.
  This token is deliberately **separate from the gate bot's approve/merge token** — DESIGN.md is
  explicit that token "never enters a build container"; this one only reads and cannot approve,
  merge, or push anything.

Built `policy-eval/verify-approvals.py` (stdlib-only Python — no dependency install needed in the
step container) and wired it into `pipelines/fast.woodpecker.yml` as a new `approval-check` step,
`event: pull_request` only. Its algorithm:

1. Fetch the PR, read its **current** `base.ref`.
2. Fetch that base's branch-protection rule (`required_approvals`, `enable_approvals_whitelist`,
   `approvals_whitelist_username`). No rule at all ⇒ `required_approvals = 0`, nothing to enforce.
3. Fetch the PR's timeline; find the latest `change_target_branch` event, if any, as `last_retarget_at`.
4. Fetch all reviews. For each reviewer, take their **most recent** review only (a later
   `REQUEST_CHANGES` supersedes an earlier `APPROVED`, matching Gitea's own semantics rather than a
   naive "any approval ever" count).
5. A review counts only if **all** hold: state is `APPROVED` and not dismissed; reviewer ≠ PR author;
   `submitted_at` is strictly after `last_retarget_at` (or no retarget ever happened); reviewer is in
   the whitelist, if one is configured.
6. Exit 0 if valid, distinct approvers ≥ `required_approvals`; exit 1 otherwise, printing exactly
   which reviews were rejected and why.

Scope kept deliberately narrow, matching SADR-0003's decision item 2 and nothing more:
`approvals_whitelist_teams` is not resolved (username whitelist only); `REQUEST_CHANGES`/`COMMENT`
reviews are left entirely to Gitea's own merge-check, this script only re-verifies the `APPROVED`
side of the bypass.

## Result — four live scenarios, same repo/branch/protection setup as SADR-0003

**1. The exact SADR-0003 attack** — bob approves against `staging`, alice retargets to `main`:

```
policy-eval: PR #1 -> base 'main', required_approvals=1, approvals_whitelist=['bob']
policy-eval: base last changed at 2026-08-24T16:34:57+00:00 -- approvals submitted at or before this do not count
  REJECTED  bob  approved 2026-08-24T16:34:42+00:00: submitted 2026-08-24T16:34:42+00:00, at or before
            the last retarget (2026-08-24T16:34:57+00:00) -- predates the current base
policy-eval: valid approvals = 0 / required = 1
policy-eval: FAIL -- get a fresh review against the current base
EXIT CODE: 1
```

Where Gitea's own `GET .../reviews` still reports `official: true, stale: false` for this exact
review (re-verified before this run) — the merge succeeds through Gitea's native check, exactly as
SADR-0003 showed, and `policy-eval` is the thing that catches what Gitea does not.

**2. The legitimate remediation** — bob approves again, now that the PR actually targets `main`:

```
  counted   bob  approved 2026-08-24T16:39:12+00:00
policy-eval: valid approvals = 1 / required = 1
policy-eval: PASS
EXIT CODE: 0
```

**3. Whitelist rejection, independent of any retarget** — a fresh PR opened directly against `main`
(no retarget in its history at all), approved only by `carol`, a genuine `write` collaborator who is
*not* on `main`'s approvals whitelist:

```
policy-eval: PR #2 -> base 'main', required_approvals=1, approvals_whitelist=['bob']
  REJECTED  carol  approved 2026-08-24T16:40:13+00:00: not in base 'main''s approvals whitelist
policy-eval: valid approvals = 0 / required = 1
EXIT CODE: 1
```

Confirms the whitelist re-derivation is a real, independent check, not an artifact of the retarget
logic — and confirms no spurious "base was retargeted" message appears when no retarget occurred.

**4. Mixed reviewers, no retarget** — bob (whitelisted) also approves PR #2:

```
  counted   bob  approved 2026-08-24T16:40:17+00:00
  REJECTED  carol  approved 2026-08-24T16:40:13+00:00: not in base 'main''s approvals whitelist
policy-eval: valid approvals = 1 / required = 1
policy-eval: PASS
EXIT CODE: 0
```

Confirms a rejected reviewer and a valid one are handled independently and correctly in the same run.

**Confirmed.** The exact bypass SADR-0003 demonstrated is now blocked, and three further cases rule
out a fix that merely pattern-matches the one scenario already known.

## Decision

1. **SADR-0003's decision item 2 is done.** `policy-eval/verify-approvals.py` exists, is wired into
   `pipelines/fast.woodpecker.yml` as the `approval-check` step (`event: pull_request`), and is
   live-tested against the attack, the remediation, and two further independent cases.
2. **Token separation is load-bearing, not incidental.** `POLICY_EVAL_GITEA_TOKEN` must stay
   read-only (`read:repository, read:issue`) on an admin-permission account, provisioned as its own
   Woodpecker secret (`policy_eval_gitea_token`) — never reuse the gate bot's approve/merge token
   here, and never widen this token's scopes without re-reading DESIGN.md's token-scoping section
   first.
3. **A real gap, stated plainly, not glossed over:** Woodpecker's `pull_request` trigger fires on
   PR open/synchronize, **not** on a review being submitted. After bob's remediating approval in
   scenario 2 above, nothing re-triggers the pipeline automatically — the stale `FAIL` commit status
   persists until *something* causes a re-run (a new push, or a manual rerun). This is exactly the
   gap D4's reconciliation loop and D5's sidecar `/rescan` command are designed to close, and neither
   exists yet. Until then, a developer who gets a fresh approval must also push an empty commit or
   manually re-run the build to see the gate flip — worth calling out in onboarding docs once
   `onboard-repo.sh` starts turning `required_approvals` above 0 for real repos, not just pilots.
4. **`onboard-repo.sh` still sets `required_approvals: 0`** (Milestone 1 scope, unchanged by this
   work) — this step is safe to ship now regardless, because at `required_approvals: 0` it always
   passes (`required <= 0` short-circuits before any review logic runs). It becomes load-bearing the
   moment a repo's protection rule requires real approvals, which is exactly the Milestone 3 sequencing
   DESIGN.md already calls for (pilot repos first, wider rollout after Milestone 4's exception path
   exists).
5. **Team-based whitelists (`approvals_whitelist_teams`) remain untested and unhandled** — flagged
   in the script's own docstring, not silently assumed to work. Add before any onboarded repo relies
   on a team whitelist rather than a username whitelist.
6. This experiment becomes a permanent regression test alongside SADR-0001's and SADR-0003's, per
   DESIGN.md's Verification section: "PR retargeted from an unprotected branch with a prior approval
   ⇒ merge still blocked without a fresh, base-aware review" now has a concrete implementation to
   regression-test, not just a documented requirement.

## Reproduce it yourself

```
cd compose/forgery-test
docker compose up -d
# wait for http://localhost:3500/api/healthz to return "pass"
# create gateadmin (admin), alice, bob, carol; mint tokens for each (bob/alice/carol:
#   write:repository; a read-only read:repository+read:issue token for gateadmin, used
#   as POLICY_EVAL_GITEA_TOKEN)
# create a repo, add alice+bob+carol as write collaborators; create an unprotected
#   "staging" branch off main; protect main: required_approvals=1,
#   approvals_whitelist=["bob"]
# alice: open a PR (via a real git clone+push, not the contents API -- the contents API's
#   new_branch_name behaved unexpectedly live, see the shell history this SADR was written
#   from) targeting staging; bob: approve it
# PATCH the PR's base to "main"
# GITEA_URL=... POLICY_EVAL_GITEA_TOKEN=... CI_REPO_OWNER=gateadmin CI_REPO_NAME=<repo> \
#   CI_COMMIT_PULL_REQUEST=<n> python3 policy-eval/verify-approvals.py
#   -> exits 1, rejects bob's stale approval
# bob approves again, now that the PR targets main; re-run the script -> exits 0
docker compose down -v
```

Nothing from a run is meant to persist — `data/` is gitignored and this is a test harness, not a
deployment profile.
