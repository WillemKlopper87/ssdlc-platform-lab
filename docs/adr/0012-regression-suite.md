# SADR-0012: Five proven experiments become an actual regression suite — 27 assertions, both harnesses, all live

**Status:** Confirmed by experiment. All 27 assertions pass, twice reproduced clean from a cold harness.
**Date:** 2026-08-25
**Milestone:** 3 (Enforcement) — closes the `docs/TODO.md` item asking for this explicitly
**Security & Privacy Impact:** Low directly (test infrastructure, not the gate itself). Indirectly
significant: every bypass this platform has closed so far was previously provable only by a human
re-typing a SADR's "Reproduce it yourself" section by hand — this is what makes "did we regress"
answerable by running a script instead.

## Question

`docs/TODO.md`, written in SADR-0010/0011's wake, named this plainly: "Turn each proven experiment
into a permanent, automated regression test... Currently all are reproducible by hand only." Five
SADRs (0001, 0002, 0003, 0009, 0011) each prove a real bypass is closed, live, once — but none of
that evidence survives past the session that produced it. This builds `tests/regression/`: one
executable per SADR, a shared helper library, and two harness runners (the lightweight Gitea-only
`forgery-test` profile for four of the five; the full `compose/minimal` stack — Postgres, Gitea,
Woodpecker — for SADR-0011's bot-approver test, which needs real pipeline runs).

## Method

Transcribed each SADR's own "Reproduce it yourself" section into an assertable script, reusing the
exact API calls and account/token patterns already proven in this project rather than re-deriving
them. Built a shared `lib.sh` first (user/token bootstrap, a real-git-push helper, health-wait
functions) so each of the five test files could stay focused on its own scenario's logic. Ran each
test individually as it was written, then as part of the full suite, then re-ran the full suite
clean — deliberately not trusting "it worked once."

## Eight real bugs found live, none of them hypothetical

**1. `docker compose down -v` does not clean `forgery-test`'s bind-mounted `./data/`.** `-v` only
removes named volumes; this harness bind-mounts a host directory instead. The suite's very first run
collided with leftover state from an earlier session's manual testing ("user already exists:
gateadmin"). Fix: the runner's cleanup step now `rm -rf`s the data directory explicitly, every run,
not just on teardown but before bringing the harness up too.

**2. The Contents API's `new_branch_name` is unreliable once the source branch has any protection at
all** — confirmed unreliable, not merely inconvenient: it silently fails once `main` carries any
branch-protection rule. Switched every test to a real `git clone` → branch → commit → push, matching
the pattern already validated in docs/adr/0009's and 0011's live sessions.

**3–4. Two different broken attempts at building a push URL by hand, before landing on the fix that
actually works.** First, bash's `${var/pattern/replacement}` silently produced a garbage URL under
plain `sh` (dash is not bash; that syntax is a bash extension) — the push URL lost its host and path
entirely (`fatal: repository 'http://localhost:3500/' not found`, but pointed at nothing). The POSIX-
correct `${var#pattern}` fix that followed then dropped the repository *path* instead — it only ever
had `host:port`, never `/owner/repo.git`, since a hand-built URL has no way to know that path. **The
actual fix**: don't rebuild a URL at all. Push to `origin` — already correctly pathed from the clone
— with the token as an `Authorization` header instead. Centralised as `git_push_origin` once found,
so no other test could rediscover either broken version.

**5. `set -e` semantics, twice, in ways that are easy to get wrong even understanding the rule.**
`set -e` fires the instant a bare simple command fails — not "when the function containing it
eventually returns". A `_run_policy_eval` helper whose whole point is to return exit 1 in the FAIL
scenarios first tried appending a `POLICY_EVAL_EXIT=$?` line after the python3 call: this does
nothing, because `set -e` aborts the whole script *at* the failing python3 command itself, before
that next line is ever reached. The same shape of bug then hit `wait_for_woodpecker_healthy`, where a
bare `code=$(curl ...)` assignment aborted the suite on the very first connection-refused retry,
before the loop got a chance to work. **The only fix that actually works**: explicit `set +e` /
`set -e` bracketing the exact command that's allowed to fail, every time — not just capturing `$?`
afterward.

**6. `_wp_pipelines` was called with no arguments from two of its three call sites.** `set -u` turned
this into an immediate, clear `1: unbound variable` rather than a silent wrong answer — worth noting
as a case where `set -u` earned its keep by converting a logic bug into a loud one.

**7. PR *creation*, not only pushes, needs the `Host: gitea:3500` header.** SADR-0008 documented this
for pushes; SADR-0011 found it recurs for PR creation specifically. This suite forgot it a third
time, independently, writing the bot-approver test — confirming this really is the kind of mistake
that costs someone the same hour repeatedly unless it's enforced by code, not just documented prose.
No structural fix applied yet beyond the comment at each call site — tracked below.

**8. `bot-approver.py` running as a plain host process (this test's context) can't resolve `gitea` as
a hostname** — that only resolves from inside `compose/minimal`'s own docker network, where the
script would run in real deployment. Not a bug in the script; a test-environment distinction the test
itself needed to account for (use the host-reachable URL when driving the script from the host).

## Result

```
=== regression summary: 18 passed, 0 failed ===   (run-forgery-suite.sh: SADR-0001/0002/0003/0009)
=== regression summary: 9 passed, 0 failed ===    (run-minimal-suite.sh: SADR-0011, incl. a real merge)
```

27 assertions total, across five independent bypass-and-fix scenarios, each reproduced from a
completely cold harness (fresh containers, fresh accounts, fresh repos every run) rather than
relying on any hand-preserved state. The bot-approver suite in particular exercises the full loop
this platform's Milestone 3 core mechanism depends on: a real pipeline run, a real bot vote, a real
pipeline restart, and a real Gitea merge, confirmed independently via a fresh `GET` after the `POST`
rather than trusting the write response alone.

## Decision

1. **`tests/regression/` is the project's first real automated verification layer**, closing the
   `docs/TODO.md` item that asked for it. `run-forgery-suite.sh` (fast, four SADRs) and
   `run-minimal-suite.sh` (slower, needs Woodpecker, one SADR) are both safe to run repeatedly — each
   backs up/restores or force-cleans whatever state it touches.
2. **`git_push_origin` in `lib.sh` is now the one correct way to push authenticated in this project's
   scripts** — never rebuild a credentialed URL by hand again; both broken attempts are preserved in
   comments specifically so nobody re-derives them from scratch.
3. ~~**The `Host: gitea:3500` requirement has now cost real time three separate times**~~ — **fixed**,
   same session, immediately after this SADR was first committed. See Addendum below.
4. **Not yet wired into CI.** These scripts are runnable, not yet *run automatically* — no Woodpecker
   pipeline or scheduled job invokes them today. That is the natural next step once `docs/OPERATIONS.md`
   or a dedicated `scripts/e2e.sh` exists to host the wiring, per DESIGN.md's own Verification section
   ("becomes a permanent regression test") — tracked, not done here.
5. **DefectDojo/exception-flow scenarios from DESIGN.md's Verification section remain out of scope**
   for this suite, correctly: they depend on Milestone 4 infrastructure (the exceptions repo, the
   sidecar) that doesn't exist yet. Extend this suite when that infrastructure lands, not before.

## Addendum (same session): the Host header is now structurally fixed, not just documented

Asked directly whether this was worth fixing rather than tracking again. It was — a fourth
documentation reminder was never going to be different from the first three.

**What didn't work, considered and rejected:** rewriting all ~90 curl call sites across the five test
files to add `-H "Host: gitea:3500"` individually. Mechanically risky (a multi-line curl invocation
is not reliably sed-able) and philosophically the same failure mode again — it relies on every
*future* call site remembering it too.

**What actually works:** `lib.sh` now defines a shell function named `curl` that shadows the real
command. Every existing `curl ...` invocation in every sourced test file — no call site rewritten —
now automatically carries the header whenever its URL targets port `:3500` (Gitea's port in every
compose profile in this project; Woodpecker is always `:8000`, so nothing else matches). Falls
through to `command curl` untouched for everything else. `git push`'s own HTTP calls go through
libcurl directly, not the shell `curl` command, so `git_push_origin` still sets the header explicitly
via `-c http.extraHeader=` — but unconditionally now, the optional parameter removed entirely, since
there was never a real case in this suite where a push shouldn't carry it.

Removed the two manual `-H "Host: gitea:3500"` additions this same SADR's own work had just added to
`05-bot-approver.sh`'s PR-creation calls — redundant now, and risky to leave: a request carrying the
same header twice is exactly the kind of thing that's fine until the day it isn't.

**Re-verified, not assumed:** both suites re-run clean after the change —
`18 passed, 0 failed` and `9 passed, 0 failed`, the same 27/27 as before the refactor.

## Reproduce it yourself

```
cd tests/regression
./run-forgery-suite.sh    # SADR-0001, 0002, 0003, 0009 -- ~2-3 minutes
./run-minimal-suite.sh    # SADR-0011 -- slower, brings up full Woodpecker
```

Both are self-contained: fresh accounts/repos every run, harness torn down (and, for the minimal
suite, any pre-existing `compose/minimal/.env` restored) on exit regardless of pass or fail.
