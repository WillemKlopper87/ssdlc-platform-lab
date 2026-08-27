# SADR-0007: DAST wired into a real scheduled Woodpecker pipeline — automated, live-tested

**Status:** Confirmed by experiment
**Date:** 2026-08-24
**Milestone:** 1 — closes the automation gap SADR-0006 explicitly left open
**Security & Privacy Impact:** Low. Confirms an automation mechanism works; no new decision changed.

## Question

[SADR-0006](0006-dast-zap-juice-shop.md) proved ZAP's baseline scan produces real findings against a
real target, run by hand. Its own Decision section named the remaining gap plainly: "not yet wired
into automation... the scan mechanism is proven, its automation isn't, yet." This experiment closes
that gap — a real Woodpecker cron job, triggering a real scheduled pipeline, scanning a persistent
staging target, with no scanner command run manually.

## Method

Added `staging-target` (the same `bkimminich/juice-shop:v20.2.0` image) as a standing service in
`compose/minimal/docker-compose.yml`, on the same network Woodpecker's step containers already use —
playing the role DESIGN.md assigns to "pre-prod staging" in this local profile. Wrote
`pipelines/dast-scheduled.woodpecker.yml`, committed it into a fresh test repo, activated the repo in
Woodpecker, registered a Woodpecker cron job (`POST /api/repos/{id}/cron`), enabled it, and triggered
it on demand (`POST /api/repos/{id}/cron/{id}`) rather than waiting for the schedule — the same
"trigger now instead of waiting" pattern already used for this platform's other live tests.

## Four real bugs found and fixed, in the order encountered — each is a genuine Woodpecker mechanic, not a mistake unique to this run

### 1. `secrets: [name]` is not valid syntax in this Woodpecker version

The linter flagged it directly: `"Additional property secrets is not allowed"`. The pipeline still
ran — as a warning, not a blocking error — but the referenced variable was simply never populated.

### 2. The corrected syntax (`environment: { VAR: { from_secret: name } }`) also failed, for a reason not fully isolated

This is the syntax Woodpecker's own e2e test suite uses verbatim
(`e2e/scenarios/restart_test.go`), and the secret's server-side gating logic
(`server/pipeline/items.go`'s `AllowedPlugins`, and the event/image matching in
`pipeline/frontend/yaml/compiler/compiler.go`) read correctly for this secret's configuration
(`images: null` → no restriction; `events: ["cron"]` → matched the triggering event exactly). The
variable still arrived empty in the running container. Root cause not fully isolated — logged
honestly as unresolved rather than papered over, since a wrong explanation would be worse than none.
Also, on reflection, the wrong tool for the job regardless: a local container hostname is
configuration, not a credential, and Woodpecker's *secret* store is arguably not what should hold it.

### 3. A plain (non-secret) `environment:` value, referenced as `${VAR}`, also arrived empty

Same symptom, different mechanism this time: the step's own `bash -x` trace showed the substitution
had *already* happened to an empty string before bash ever evaluated the command — meaning
Woodpecker's own compiler is consuming `${...}`-shaped text server-side in this version, not leaving
it for shell-runtime expansion, regardless of the Drone-style `$${VAR}` escape (tried and also
empty). This is architecturally distinct from bug 2 (no secret involved at all) and equally
unresolved. **Decision: stop chasing a general parameterization mechanism.** This profile only ever
targets one known local service; a literal value in the command does the actual job.

### 4. Writing the literal `${...}` pattern into an explanatory *comment*, to document bugs 2 and 3, broke the pipeline outright

Not a hypothetical — this happened. The very act of writing "referenced as `${VAR}`" in a YAML
comment produced a **blocking** parse error on the next trigger: `"unable to parse variable name"`.
This confirms bug 3's mechanism precisely: Woodpecker text-substitutes dollar-brace patterns across
the **entire file, comments included**, before real YAML parsing — a comment is not a safe place to
even *discuss* the syntax that broke. Fixed by rephrasing the comments to describe the pattern
without ever writing the literal characters, which is why the current file's prose reads slightly
around the houses in places — that phrasing is deliberate, not accidental.

### 5. `zap-baseline.py`'s own `-J`/`-r` file-output flags require `/zap/wrk` to be a genuine host bind mount — which it never is inside a Woodpecker-orchestrated step

The same warning SADR-0006 saw on its very first, pre-fix standalone attempt reappeared here in a
different context: `"A file based option has been specified but the directory '/zap/wrk' is not
mounted"`, followed by ZAP printing usage/help instead of scanning. In the standalone `docker run`
case, the fix was mounting a real volume at that exact path. Inside a Woodpecker step, there is no
equivalent lever — Woodpecker manages the shared pipeline workspace itself, mounted at its own path,
which is never `/zap/wrk`. **Fix:** drop `-J`/`-r` entirely; redirect ZAP's normal console output to
a plain file (`> zap-console.txt`, ordinary shell redirection, no special mount requirement) and have
the summary step `grep` the `PASS`/`WARN-NEW`/`FAIL-NEW` lines — exactly the console format
SADR-0006's very first standalone run already demonstrated parsing correctly, before that experiment
ever added the `-J`/`-r` flags at all.

## Result

```
POST /api/repos/1/cron/1                    → 200, pipeline #10, event=cron
GET  /api/repos/1/pipelines/10
  clone:         success
  dast-baseline: success
  summary:       success

Summary step output:
  WARN-NEW: Content Security Policy (CSP) Header Not Set [10038] x 5
  WARN-NEW: Non-Storable Content [10049] x 9
  WARN-NEW: Deprecated Feature Policy Header Set [10063] x 5
  WARN-NEW: Timestamp Disclosure - Unix [10096] x 5
  WARN-NEW: Cross-Domain Misconfiguration [10098] x 4
  WARN-NEW: Modern Web Application [10109] x 5
  WARN-NEW: Dangerous JS Functions [10110] x 1
  WARN-NEW: Cross-Origin-Embedder-Policy Header Missing or Invalid [90004] x 10
  FAIL-NEW: 0  FAIL-INPROG: 0  WARN-NEW: 8  WARN-INPROG: 0  INFO: 0  IGNORE: 0  PASS: 59
```

**Confirmed.** The findings closely match SADR-0006's manual scan (same 8 alert types, same
severities) — this is the same real scanner behaviour, now reached through a genuinely automated
path: a cron trigger, a persistent target, a pipeline no human ran a scanner command inside of. The
push-triggered pipelines created alongside each fix commit (correctly, via the `when: event: cron`
filter) ran only their `clone` step and stayed green — confirming the scheduled pipeline does **not**
fire on ordinary pushes, matching DESIGN.md's placement of DAST as separate from the fast gate.

**A secret-redaction side effect worth knowing about, not a bug:** once the `dast_target_url` secret
existed (bug 1/2's leftover), Woodpecker's log redaction masked every later occurrence of that exact
string value in *any* log — including the literal-value version that no longer referenced the secret
by name at all. `Scanning target: ********` is Woodpecker correctly protecting a value it once saw
registered as a secret; not a functional failure, but a real thing to expect if a secret is created
and later abandoned in favour of a literal.

## Decision

1. **The DAST automation gap SADR-0006 left open is closed.** Both scanning legs named across this
   session's "go build and live-test SCA and DAST" request now have real, automated evidence.
2. **`pipelines/dast-scheduled.woodpecker.yml` is the second real, working paved-road artifact**,
   alongside `pipelines/fast.woodpecker.yml` — carry forward as-is.
3. **Two genuinely unresolved Woodpecker mechanics remain open** (bugs 2 and 3 above): why
   `from_secret` didn't populate despite matching the framework's own test fixture, and the exact
   scope/rules of Woodpecker's server-side `${...}` template substitution. Neither blocks this
   platform's current needs, but both are real gaps in understanding, not swept under anything —
   tracked here for whoever next needs real secret-backed parameterization in a Woodpecker pipeline.
4. **Never write a literal `${...}`-shaped string anywhere in a `.woodpecker.yml` file, including
   comments**, unless it's a value Woodpecker is meant to substitute. Add this as an explicit rule in
   `docs/OPERATIONS.md` once that document exists — it is exactly the kind of gotcha that costs
   someone else the same hour it cost this session.
5. Any future Woodpecker step needing ZAP's structured JSON/HTML report output (rather than console
   text) will need a different mechanism than `-J`/`-r` — e.g., writing to a path known to be within
   Woodpecker's actual workspace mount, if one can be identified, or capturing output some other way.
   Not investigated further here; console-text parsing was sufficient for this milestone's need.

## Reproduce it yourself

```
cd compose/minimal
cp .env.example .env   # POSTGRES_PASSWORD, WOODPECKER_AGENT_SECRET, WOODPECKER_GRPC_SECRET
docker compose up -d postgres
# wait healthy, then:
docker compose up -d gitea staging-target
# wait http://127.0.0.1:3500/api/healthz == "pass"
# create admin, mint a token, register an OAuth2 app (redirect_uri
#   http://127.0.0.1:8000/authorize), put creds in .env
docker compose up -d woodpecker-server woodpecker-agent
# log in (see docs/adr/0004's method), activate a repo, commit
# pipelines/dast-scheduled.woodpecker.yml as .woodpecker.yml
curl -X POST .../api/repos/{id}/cron -d '{"name":"scheduled-dast","schedule":"0 2 * * *","branch":"main"}'
curl -X PATCH .../api/repos/{id}/cron/{cron_id} -d '{"enabled":true}'
curl -X POST .../api/repos/{id}/cron/{cron_id}   # trigger now instead of waiting
docker compose down -v
```
