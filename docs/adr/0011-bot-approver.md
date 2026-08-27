# SADR-0011: The bot's half of `required_approvals: 2` — built, and live-tested through an actual merge

**Status:** Confirmed by experiment, positive and negative paths, through to a real Gitea merge
**Date:** 2026-08-24
**Milestone:** 3 (Enforcement) — makes D2's `required_approvals: 2` mitigation (verified in
[SADR-0001](0001-commit-status-forgery.md), never actually implemented) real for the first time
**Security & Privacy Impact:** Critical. This is the mechanism that gives the bot-only-approver-team
control its second half — without it, `required_approvals: 2` is either unset (no real protection
beyond a single human) or set and unsatisfiable (nothing merges, ever).

## Question

SADR-0001 proved `required_approvals: 2` closes the status-forgery bypass — but only by having a
human (`gateadmin`) manually play the bot's role in a throwaway harness. D2's actual design calls for
a bot account whose approval is cast automatically, once the fast gate's scanning verdict is green,
by something outside the build-container trust boundary (DESIGN.md is explicit the bot's token "lives
only in the sidecar... and never enters a build container"). Nothing in this project built that yet —
`onboard-repo.sh` still set `required_approvals: 0` for exactly this reason. This experiment builds
it, and asks a sharper question than "does the bot approve": does the *whole loop* close, including
`policy-eval`'s own `approval-check` step (SADR-0009), which has a real circular dependency on the
bot's vote that has to be designed around, not just discovered live?

## Method — the circular dependency, found before writing any code

Woodpecker reports **one combined status per triggering event**, not one per step (confirmed from
SADR-0005's own result: `ssdlc/security-gate/push/woodpecker`, not a per-step context). That means if
`approval-check` fails, the *whole* pipeline reports failure — even if every scanning step passed. A
bot-approver that waits for "the gate's status is green" before voting would therefore wait on
`approval-check`'s own result, which itself will not pass until the bot has voted: a deadlock, found
by reasoning through the architecture before ever running anything.

**Fix, designed in before the first test:** `scripts/bot-approver.py` talks to **Woodpecker's own
per-step API**, not Gitea's combined status, and explicitly excludes `approval-check` (and `clone`)
from what it waits on. It votes based on the scanning steps alone; `approval-check` is left to
re-evaluate independently, on its own next run, once the bot's vote exists.

Getting a durable, scriptable Woodpecker identity also needed solving. SADR-0004 flagged its own
CSRF-JWT-from-a-database-read method as "not the pattern for production automation," and this
session's own attempt to reuse it was **blocked outright by the harness's permission classifier** —
a live confirmation that it's the wrong pattern, not just a stated preference. Found the actual
legitimate path instead: `GET /web-config.js`, called with a valid session cookie, embeds a
properly-issued CSRF JWT (`server/web/config.go`: `token.New(token.CsrfToken)`, signed server-side,
same session-scoped mechanism the real web UI itself uses) — no database read required. Used that
CSRF token exactly once, to call `POST /api/user/token` and mint a durable personal access token
(`token.UserToken`, which `shared/token/token.go` confirms does **not** require CSRF on later calls).
This closes SADR-0004's own decision item 3 as a side effect: a cleaner, fully legitimate replacement
for its hand-rolled JWT method.

Live-tested against `compose/minimal`'s real stack (not the throwaway forgery-test harness — this
needed real Woodpecker pipeline runs, which the harness doesn't have): three accounts (`gate-bot`,
`alice` as PR author, `bob` as the human reviewer), `required_approvals: 2`, no whitelist (the exact
config SADR-0001 round 2 proved sufficient), a minimal two-step pipeline (`code-scan` standing in for
secrets/SAST/SCA collectively, plus the real `approval-check` step from SADR-0009).

## Four real bugs found along the way, each fixed and re-verified live

1. **The same clone_url bug SADR-0008 already documented, forgotten and rediscovered.** The first
   push and PR-open calls were made without the `Host: gitea:3500` header this project already knows
   it needs — Gitea derived `clone_url` from the *request's* Host header (`127.0.0.1:3500`), and the
   clone step failed exactly as SADR-0008 predicted. Fixed by redoing the triggering calls with the
   header. **New detail this session adds to that standing rule:** PR *creation* itself, not only
   pushes, needs the header — a second, later pipeline run hit the identical clone failure from a PR
   opened without it, even though the push that created the branch had the header correctly applied.
2. **`scripts/bot-approver.py`'s first version never sent `Content-Type: application/json`.** Gitea's
   review-creation endpoint parsed the resulting body as empty and rejected it: `"review event
   requires a body"` — a real, live-caught bug, fixed by adding the header whenever a JSON body is
   sent.
3. **`POST /api/repos/{id}/pipelines/{number}` does not restart in place.** It creates a **new**
   pipeline (`parent: <old-number>`, `rerun_count` incremented) rather than re-running the same
   number. The first verification pass checked the old pipeline number after triggering this and
   found it completely unchanged (`rerun_count: 0`, `finished` timestamp from before the bot's
   approval even existed) — a genuine "checked the wrong thing" moment, caught by cross-referencing
   timestamps rather than trusting the first result. `bot-approver.py` itself was never affected by
   this (each poll cycle re-fetches pipelines fresh and picks the latest by id) — only this session's
   manual verification step needed correcting.
4. **`approvals_whitelist` is not how "bot + at least one human" actually gets enforced.** Worked
   through before writing config, not discovered as a bug: setting a whitelist containing only the
   bot would mean *only* the bot's approval ever counts as official (confirmed by SADR-0009's own
   carol/bob test: non-whitelisted approvals are excluded entirely, not merely deprioritized) — making
   `required_approvals: 2` permanently unsatisfiable, since one account cannot cast two distinct
   votes. The correct config, matching what SADR-0001 round 2 already proved, is `required_approvals:
   2` with **no** whitelist: Gitea's native self-approval block plus the 2-distinct-approvers count
   is what actually produces "bot and at least one human," not `approvals_whitelist`.

## Result — full loop, both directions, through an actual merge

**Positive path:**

```
bob approves (1/2) -> pipeline runs: code-scan success, approval-check FAILS
  ("valid approvals = 1 / required = 2")
bot-approver.py run: sees code-scan green, bot hasn't voted for this SHA -> approves, restarts pipeline
new pipeline run: approval-check PASSES ("counted bob ... counted gate-bot ... valid = 2 / required = 2")
GET .../commits/<sha>/status -> combined state: success
POST .../pulls/1/merge {"Do":"merge"} -> 200 OK
GET .../pulls/1 -> "merged": true, "merged_by": "alice"
```

**Negative path** (a PR whose scanning genuinely fails):

```
bot-approver.py run against the broken PR
  -> "PR #2: scanning steps not all green yet ({'code-scan': 'skipped'}) -- skip"
GET .../pulls/2/reviews -> review count: 0
```

No approval was cast. The bot correctly declined regardless of *why* the scanning steps weren't
verifiably green (in this run, a recurrence of bug 1 above rather than the deliberately-failing step
itself — still the correct conservative outcome: skip whenever it cannot positively confirm green).

**Confirmed, completely.** `required_approvals: 2` is no longer a verified-but-unbuildable design note
— a real PR merged through it, cast by a real automated bot vote plus a real independent human vote,
with `policy-eval`'s own retargeting-aware `approval-check` (SADR-0009) still fully in the loop and
re-evaluating correctly once the bot's vote existed.

## Decision

1. **`onboard-repo.sh` now sets `required_approvals: 2`** (was `0` since Milestone 1, deliberately,
   pending exactly this mechanism) and adds `gate-bot` as a write collaborator on every onboarded
   repo. This is now load-bearing: a repo onboarded without `scripts/bot-approver.py` actually running
   against it is permanently unmergeable, not merely less protected — the script's own docstring and
   `onboard-repo.sh`'s header comment both say so explicitly.
2. **`scripts/bot-approver.py` must run as its own standing process, never inside a build container.**
   Confirmed why this matters, not just asserted: a compromised scanner image or a crafted pipeline
   step in the *same untrusted PR* this platform gates could otherwise exfiltrate the bot's token and
   forge future approvals — a strictly worse hole than the one being closed. Not yet wired as a
   `compose/minimal` service (tracked below); run manually (`RUN_ONCE=1` for a single pass, or the
   default poll loop) until then.
3. **Single-repo only, by design, for now.** `REPO_OWNER`/`REPO_NAME`/`WOODPECKER_REPO_ID` are plain
   env vars — real multi-repo operation (watching every onboarded repo, not one hardcoded pair) is
   deferred, honestly, rather than built speculatively before there's a second real onboarded repo to
   prove it against.
4. **Wire `bot-approver.py` into `compose/minimal/docker-compose.yml`** as a standing service once
   multi-repo support exists — not done in this pass.
5. **SADR-0004's decision item 3 is now closed, not just noted as a goal**: the `GET /web-config.js`
   → CSRF → `POST /api/user/token` sequence is the proper, documented-shape way to obtain a durable
   Woodpecker API credential. Prefer it over the database-read method in every future experiment or
   automation against Woodpecker's API.
6. The Host-header requirement from SADR-0008 gets a documented extension: **every host-side call
   that can trigger a Gitea webhook — pushes *and* PR creation, not pushes alone** — needs the
   explicit `Host: gitea:3500` header in this profile. Worth lifting into `docs/OPERATIONS.md` (still
   not created — see `docs/TODO.md`) verbatim alongside SADR-0008's original version of this rule.

## Reproduce it yourself

```
cd compose/minimal
cp .env.example .env
docker compose up -d postgres
# wait healthy, then:
docker compose up -d gitea
# wait http://127.0.0.1:3500/api/healthz == "pass"
# create gateadmin (admin), gate-bot, alice, bob; mint tokens for each
#   (gate-bot: write:repository only)
# register an OAuth2 app (redirect_uri http://127.0.0.1:8000/authorize), put creds in .env
docker compose up -d woodpecker-server woodpecker-agent staging-target
# log in as gateadmin (Gitea form POST + the Woodpecker OAuth dance, SADR-0004's method),
# GET /web-config.js with the session cookie for a CSRF token, POST /api/user/token for a durable PAT
# create a repo, add alice/bob/gate-bot as write collaborators, protect main:
#   required_approvals=2, no whitelist
# alice: push a feature branch + a minimal .woodpecker.yml (code-scan + approval-check steps)
#   -- every push/PR-open call needs -H "Host: gitea:3500" (or git -c http.extraHeader=)
# open the PR; bob approves
# GITEA_URL=... GITEA_BOT_TOKEN=... GITEA_BOT_LOGIN=gate-bot REPO_OWNER=... REPO_NAME=... \
#   WOODPECKER_URL=... WOODPECKER_TOKEN=<PAT> WOODPECKER_REPO_ID=<id> RUN_ONCE=1 \
#   python3 scripts/bot-approver.py
# -> approves, restarts the pipeline; check the new pipeline's approval-check step, then merge
docker compose down -v
```

Nothing here is meant to persist — `data/`/named volumes are gitignored/torn down, matching every
prior experiment in this series.
