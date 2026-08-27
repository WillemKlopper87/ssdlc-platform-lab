# SADR-0013: `policy-eval` resolves team-based approval whitelists, not just usernames

**Status:** Confirmed by experiment. 4 new assertions, 22/22 across the whole forgery suite.
**Date:** 2026-08-26
**Milestone:** 3 (Enforcement) — closes the gap SADR-0009 flagged explicitly and left for later
**Security & Privacy Impact:** Medium. Closes a real coverage gap: a repo relying on
`approvals_whitelist_teams` (arguably the more common real-world configuration than a bare username
list) previously got **no re-derivation at all** from `policy-eval` — every team-whitelisted approval
was rejected as "not in whitelist," which is fail-safe (never a false PASS) but would have made
`required_approvals` unsatisfiable on any repo actually using teams.

## Question

SADR-0009's own docstring said it plainly: "`approvals_whitelist_teams` is not resolved here -- a
repo relying on a team-based whitelist is not yet covered by this check (tracked as a follow-up, not
done)." This closes that follow-up: does `policy-eval` correctly recognize a reviewer's approval as
valid when they're whitelisted via **team membership** rather than being named directly, without
opening a new gap in the process?

## Method

Gitea teams are an organization concept — every prior experiment in this project used personal-account
repos (`gateadmin/...`), so this is the first one that needed a real Gitea org. Set up live against
`compose/forgery-test`: an org, a `reviewers` team with `bob` as its sole member, a repo owned by the
org with `approvals_whitelist_teams: ["reviewers"]` and no username whitelist, `carol` added as an
ordinary write collaborator but **not** a team member.

Checked Gitea's own behavior before writing anything, not assumed:

- `GET /repos/{owner}/{repo}/teams` (list teams with access to *this* repo, name → id) needs only
  `read:repository` — already in `policy-eval`'s token.
- `GET /teams/{id}/members` needs `read:organization` specifically — confirmed live via a real `403`
  ("token does not have at least one of required scope(s), required=[read:organization]") before
  widening the token, not guessed from documentation.
- Gitea's own approval computation already gets teams right for the simple case (`bob`'s review
  showed `official: true`, `carol`'s showed `official: false`) — expected, since SADR-0003's bug was
  specifically about retargeting not recalculating `official`, not about whitelist evaluation itself.
- Gitea **won't let a nonexistent team name persist** in `approvals_whitelist_teams` at all (a `PATCH`
  naming one returns `422`), and **silently drops a team that exists but lacks repo access** from the
  stored list rather than erroring. Tested directly: revoking a whitelisted team's repo access mid-test
  caused `approvals_whitelist_teams` to auto-empty on the next read. This means the "team name in the
  whitelist that can't be resolved" branch in the fix below is defense-in-depth against a state Gitea's
  own API keeps from arising in practice, not a gap this experiment found exploitable.

Extended `policy-eval/verify-approvals.py` with `resolve_team_members()`: reads
`approvals_whitelist_teams` from branch protection, resolves each name to a team ID via the repo's own
team list (which also implicitly confirms the team has access to *this* repo — a team named in the
whitelist that isn't repo-associated can't produce members through this path, matching what Gitea
itself already enforces), fetches each team's members, and unions them into the same whitelist set
`approvals_whitelist_username` already populated. No caching — team membership, like everything else
in this script, is re-read fresh on every run.

## Result

```
Scenario 1 -- a token WITHOUT read:organization:
  policy-eval: FATAL: GET /teams/2/members -> HTTP 403: ...required=[read:organization]...
  EXIT: 2   <- fails LOUD, not a silent miscount

Scenario 2 -- with the right scope:
  policy-eval: PR #1 -> base 'main', required_approvals=1, approvals_whitelist=['bob']
    (includes members of team(s) ['reviewers'])
    counted   bob    approved ...
    REJECTED  carol  approved ...: not in base 'main''s approvals whitelist
  policy-eval: valid approvals = 1 / required = 1
  policy-eval: PASS
```

**Confirmed.** `bob`'s team-derived approval now counts; `carol`'s does not, despite her being a
perfectly ordinary write collaborator on the same repo — exactly the distinction the whitelist exists
to enforce. And confirmed the failure mode is the right one: a token missing the new required scope
doesn't quietly under-count (which would fail safe but be maddening to debug in real operation) — it
stops the whole evaluation with a clear, specific error.

Added as permanent regression coverage: `tests/regression/06-team-whitelist.sh`, wired into
`run-forgery-suite.sh`. Full suite: **22 passed, 0 failed** (18 prior + 4 new), confirming this
extension didn't disturb anything SADR-0001/0002/0003/0009 already proved.

One real bug found building the *test*, not the fix itself: `lib.sh`'s `git_push_feature_branch`
hardcoded the repo owner as `gateadmin` — every prior test used a personal-account repo, so nothing
had exercised the org-owned path before. Fixed by adding an optional `owner` parameter (default
`gateadmin`, preserving every existing call site unchanged) rather than hardcoding a second owner.

## Decision

1. **`policy-eval/verify-approvals.py` now handles both whitelist mechanisms Gitea offers.** The
   scope note in its own docstring is updated; SADR-0009's tracked follow-up is closed.
2. **`POLICY_EVAL_GITEA_TOKEN` must include `read:organization`** going forward, on top of
   `read:repository` and `read:issue` — a real, load-bearing scope widening, not optional. Any
   already-deployed token needs updating before a repo adopts a team-based whitelist, or every
   evaluation on that repo will fail closed with a clear but blocking error.
3. **Fail-loud-on-missing-scope was a deliberate design choice, tested as such.** The alternative
   (treat an unresolvable team as "no members," degrading gracefully) would silently under-count
   approvals in exactly the misconfiguration case where a human most needs a clear signal. Fail-safe
   and fail-loud aren't in tension here; both point the same direction.
4. **`approvals_whitelist_teams` naming a team without repo access is not exercised as a real failure
   path** — Gitea's own API prevents it from persisting. The script's warning branch for that case
   stays in as defense-in-depth (a different Gitea version, or a config path this project hasn't
   tried, could behave differently) but isn't claimed as independently proven here.
5. This experiment (`tests/regression/06-team-whitelist.sh`) is now part of the standing regression
   suite alongside the other five — `docs/TODO.md`'s "turn proven experiments into regression tests"
   item extends automatically to cover this one too.

## Reproduce it yourself

```
cd tests/regression
./run-forgery-suite.sh   # now runs all six: SADR-0001, 0002, 0003, 0009, 0013, plus SADR-0002's
                          # secret-gate scenarios bundled in the same pass
```

Or standalone, matching this SADR's own method:

```
cd compose/forgery-test
docker compose up -d
# create gateadmin (admin), an org, a "reviewers" team with one member, a second ordinary
# collaborator NOT on the team; create a repo under the org with approvals_whitelist_teams
# set to that team name, required_approvals=1
# open a PR, have both accounts approve
# GITEA_URL=... POLICY_EVAL_GITEA_TOKEN=<read:repository,read:issue,read:organization> \
#   CI_REPO_OWNER=<org> CI_REPO_NAME=<repo> CI_COMMIT_PULL_REQUEST=<n> \
#   python3 policy-eval/verify-approvals.py
# -> exits 0, team member counted, non-member rejected
docker compose down -v
```
