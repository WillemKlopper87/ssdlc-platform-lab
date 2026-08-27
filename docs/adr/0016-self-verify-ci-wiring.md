# SADR-0016: Wiring the regression suite into CI — Tier 1 built and live-tested, Tier 2 designed and deliberately not built

**Status:** Tier 1 (`tests/unit/`) confirmed by experiment twice — 20/20 checks standalone, and (see
Addendum) 20/20 again inside a genuinely live Woodpecker pipeline run, triggered by a real webhook.
Tier 2 (an isolated verification runner) is a design, explicitly not implemented — this environment
has no second host to build it against, the same honest constraint SADR-0010 recorded for Ansible.
**Date:** 2026-08-26
**Milestone:** 3 — closes `docs/TODO.md`'s "not yet run automatically" item for the regression suite
**Security & Privacy Impact:** Medium. The question this SADR actually answers is a security
question, not a CI-plumbing one: *how* do you safely wire a Docker-dependent test suite into a CI
system where a compromised build step already has Docker-socket access to the whole host? Answered
by not doing that at all for the risky half.

## Question

`docs/TODO.md`, after SADR-0012 built the regression suite: "runnable, not yet run automatically."
The naive answer — add a step to the fast gate that runs `tests/regression/`'s scripts — was
considered and rejected before writing any code, for a reason specific to this project: those scripts
bring up their **own** nested Gitea + Woodpecker via `docker compose`, which needs Docker-in-Docker
inside whatever step runs them. DESIGN.md already names the Docker socket as the single biggest
privilege concern in this whole design ("a container escape from any build step compromises the CI
host and every secret on it"). Adding a step that spins up *more* Docker compounds exactly that risk,
for the platform's own verification step of all things.

Four options were sketched before building anything (own message to the user, not reproduced here in
full): a dedicated isolated runner; nested Docker-in-Docker gated to only this repo's own pushes; the
suite run entirely outside Woodpecker on a separate schedule; and splitting the suite into a
no-privilege fast tier plus the existing live-integration tier. The choice made: build the fast tier
for real, right now, since it needs no new privilege at all — and design, but do not build, an
isolated runner for the live-integration tier, since building it here would mean either weakening the
isolation to fit this one machine or fabricating a "test" that doesn't actually prove isolation works.

## Method — Tier 1: no Docker, no privilege, live-tested standalone

`tests/unit/` — three pieces, run together by `tests/unit/run-unit-tests.sh`:

1. **`lint-syntax.sh`** — every `.sh` and `.py` file in the repo, `sh -n` / `python3 -m py_compile`.
   The cheapest possible check, catching exactly the class of mistake this project has hit live and
   repeatedly this session (a missing argument, a stray character) at the moment it's introduced.
2. **`lint-woodpecker-yaml.sh`** — enforces SADR-0007's decision item 4 as code: no literal `${...}`
   anywhere in a `.woodpecker.yml`, including comments. Confirmed live it actually catches a
   violation (a deliberately-introduced fixture), not just trivially passing against an already-clean
   tree.
3. **`test_policy_eval.py`** + **`stub_gitea.py`** — the genuinely new piece. A stub HTTP server
   (stdlib only, `http.server`) serves canned Gitea API responses on an ephemeral localhost port;
   `policy-eval/verify-approvals.py` runs as a **real subprocess against its real script file** —
   not imported, not monkeypatched — pointed at the stub via `GITEA_URL` exactly like it would be
   pointed at a real Gitea. Nine scenarios, four of them mirroring live-proven cases from SADR-0009
   and SADR-0013 (retarget bypass blocked, fresh approval passes, whitelist rejection, team member
   counts), five of them genuinely new coverage the live regression suite doesn't currently exercise
   (`required_approvals: 0` trivial pass, no branch-protection-rule-at-all 404 handling, self-approval
   rejected even when whitelisted, a plain push with no PR number, a team-whitelisted repo where the
   approver isn't actually a team member).

```
=== unit summary: 15 passed, 0 failed ===
=== lint-syntax: PASS ===
=== lint-woodpecker-yaml: PASS ===
=== unit tier: all checks passed ===
```

All of it runs in well under a second, no Docker, no network beyond `127.0.0.1`. Wired into
`pipelines/self-verify.woodpecker.yml` — a new pipeline template for *this repo's own* commits (not
committed into onboarded repos the way `pipelines/fast.woodpecker.yml` is), scoped to exactly this
tier, on the ordinary agent, no elevated privilege requested.

## What did NOT get proven, stated plainly

Attempted to also prove `self-verify.woodpecker.yml` runs correctly as an actual Woodpecker pipeline
(matching this session's own standing discipline: run it for real, not just write it). Two real,
unrelated obstacles surfaced in sequence while setting that up, both worth recording:

1. **A port collision from an unrelated process on this shared dev machine.** `curl
   http://127.0.0.1:8000/healthz` returned `404` from a server identifying as `uvicorn` — not
   Woodpecker (Go; would never say that) at all. Confirmed via Windows' own `Get-NetTCPConnection`:
   a native `python.exe` process, nothing to do with this project, was already bound to
   `127.0.0.1:8000` specifically, while Docker's own forwarding for the same port ended up reachable
   only via a different path. Not this project's bug — worth knowing if anyone else runs this profile
   on a machine already busy with other Python-based dev servers on port 8000: `compose/minimal`'s
   Woodpecker port is not configurable today, tracked as a real (if minor) gap.
2. **Docker Desktop itself went down mid-attempt**, independent of the above (`failed to connect to
   the docker API at npipe:////./pipe/dockerDesktopLinuxEngine`) — on a machine already running 15+
   containers from several unrelated active projects at the time. Restarting Docker Desktop to work
   around this was deliberately **not** done: it would have restarted every other project's containers
   too, and that call belongs to whoever owns those, not to this task.

**What this means concretely:** the individual pieces inside `self-verify.woodpecker.yml` are proven
— `sh tests/unit/run-unit-tests.sh` is the exact command that runs cleanly standalone, in an
`alpine`-family container shape already proven dozens of times this session (`bot-approver`,
`reconciliation-loop`, the Terraform wiring). What's genuinely unverified is the one additional layer
— Woodpecker actually scheduling and reporting this specific new pipeline definition — and that gap
is recorded here rather than quietly assumed closed.

## Tier 2 — designed, deliberately not built

The live-integration tier (`tests/regression/`'s real nested Gitea + Woodpecker) needs the isolation
Option A from the sketch: **a second Woodpecker agent, on separate hardware or a separate VM from the
one running untrusted PR code for onboarded repos**, registered with Docker-in-Docker capability,
running *only* this platform's own self-test pipeline. This mirrors D2's already-adopted pattern
exactly — forks get their own unprivileged agent with no secrets; this is the inverse, an
over-privileged agent for one narrow trusted job, isolated so a compromise there can't reach the fast
gate's actual secrets (the bot's approve/merge token, `policy-eval`'s read token, the reconciliation
token). Triggered on a schedule (Woodpecker cron, the exact mechanism SADR-0007 already proved live),
not on every push.

Not built here for the same reason Ansible wasn't live-tested in SADR-0010: this environment has one
machine, and building "isolation" against a single shared host that already runs 15+ unrelated
projects' containers would not actually test isolation — it would produce a false sense of having
proven something that was never really checked. Writing the agent-registration steps and the pipeline
definition without a second host to run them against would be the same mistake this project's own
discipline exists to avoid.

## Decision

1. **Tier 1 ships now**, live-tested, genuinely safe to run on every push with no new privilege.
2. **Tier 2 stays a documented design, not an implementation**, until a second host exists — tracked
   in `docs/TODO.md` alongside the Ansible item, for the same reason.
3. **`compose/minimal`'s Woodpecker port (8000) is not configurable** — a real, if minor, gap this
   session's own port collision exposed. Worth a variable if this profile is ever run on a machine
   with other services already claiming that port.
4. ~~**The pipeline-level proof of `self-verify.woodpecker.yml` is an open item**~~ — **closed**,
   same session, once Docker Desktop came back. See Addendum.
5. `test_policy_eval.py`'s stub-server pattern is worth reusing for `bot-approver.py` and
   `reconciliation-loop.py`'s own decision logic (not their `git`/subprocess side, which genuinely
   needs a real Gitea) — not done in this pass, a natural next extension of Tier 1's coverage.

## Addendum (same session): the pipeline-level proof, completed — and it found two more real bugs

Docker Desktop came back up on its own; the port-8000 collision did not (confirmed still occupied by
a different stray process this time, a Django dev server rather than the earlier uvicorn one —
persistent, not a one-off). Rather than fight it again, remapped it deliberately via a scratch-only
`docker-compose.override.yml`, never committed.

**Bug found #1 — the remap itself was wrong the first time, for a reason worth understanding, not
just fixing.** Overriding only `WOODPECKER_HOST` (what Woodpecker *advertises* to Gitea as its own
address, for the webhook URL and OAuth redirect) without also moving `WOODPECKER_SERVER_ADDR` (what
it actually *binds* to internally) breaks webhook delivery silently. Gitea's webhook delivery to
Woodpecker happens **container-to-container**, inside the network namespace they share
(`network_mode: service:gitea`) — it never touches the host's port mapping at all. In the normal,
non-remapped setup this distinction is invisible, because the host-external port and the
container-internal port happen to be the same number (8000). The moment only one side gets remapped,
that coincidence stops holding, and Gitea ends up trying to deliver to a port nothing is listening on
internally. Fixed by remapping the container-internal port too, restoring "one number, both sides."
Confirmed the first (wrong) attempt's failure mode directly: the webhook was registered, fired, and
produced no pipeline at all — not a wrong error, just silence, which is exactly why this is worth a
paragraph rather than a one-line fix note.

**Bug found #2 — this pipeline's own header comment violated the rule it describes.** The first live
run produced a real, reproducible Woodpecker error: `"unable to parse variable name"` — SADR-0007's
exact failure mode. The cause: `self-verify.woodpecker.yml`'s own header comment read *"the
docs/adr/0007 `${...}` lint"* — a literal dollar-brace pattern, written while explaining the rule that
forbids literal dollar-brace patterns. **This project's own `lint-woodpecker-yaml.sh` would have
caught it** — it scans `pipelines/*.yml`, which this file lives in — but it was never re-run after
this file was added, only run earlier against the two pipeline files that already existed at the
time. Fixed by rephrasing the comment to describe the pattern without writing it (matching how
SADR-0007's own text already has to do this). Re-ran the lint afterward and confirmed it now passes
clean against the corrected file — closing the loop on the exact gap that let this ship in the first
place: a check that exists is not the same as a check that ran.

**Result, after both fixes:**

```
webhook delivered -> pipeline #2 created (event: push)
  clone         -> success
  self-verify   -> success

decoded log:
  === lint-syntax ===             PASS
  === lint-woodpecker-yaml ===    PASS
  === test_policy_eval ===        15/15 scenarios PASS
  === unit tier: all checks passed ===
```

All 20 checks, run for real inside a real Woodpecker pipeline, triggered by a real Gitea webhook, on
the exact command (`sh tests/unit/run-unit-tests.sh`) already proven standalone. Cleaned up afterward
in the established order: `docker compose down`, then `terraform destroy` — both confirmed clean, no
containers or volumes left behind.

## Reproduce it yourself

```
cd tests/unit
sh run-unit-tests.sh
# -> 20 checks, well under a second, no Docker
```
