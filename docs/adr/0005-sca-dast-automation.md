# SADR-0005: SCA automation, live-tested end-to-end through onboard-repo.sh

**Status:** Confirmed by experiment
**Date:** 2026-08-24
**Milestone:** 1 (Forge + CI), extends the D1 loop with real automation and a real caught CVE
**Security & Privacy Impact:** Medium. Closes the "SCA not live-tested" gap and proves the
zero-configuration developer story the platform exists to deliver.

## Question

Two things needed proving together, not separately: (1) does Trivy, wired into the fast-gate
pipeline, actually catch a real vulnerable dependency and block the merge, and (2) can a developer
get that protection with **zero interaction with Woodpecker or Gitea configuration** — clone, code,
push, read the result? The second question is the actual point of the platform; the first is table
stakes.

## Method

Built two new artifacts, both real, both committed:

- `pipelines/fast.woodpecker.yml` — the pipeline template every onboarded repo receives automatically:
  Gitleaks → Semgrep (`p/security-audit` + `p/secrets`, the same ruleset validated in this session's
  live Semgrep demo) → Trivy filesystem scan → a summary step.
- `scripts/onboard-repo.sh` — the platform-admin automation that activates a repo in Woodpecker,
  commits the pipeline template into it, and sets branch protection. This is the script standing
  between "here is a repo" and "every push is now scanned automatically" — a developer never runs it,
  never sees it, never needs to know it exists.

Brought up `compose/minimal` fresh, ran the full OAuth login (SADR-0004's proven method), then:

1. Ran `onboard-repo.sh gateadmin sca-dast-test` — **on the first try, clean, no errors.**
2. Cloned the repo as a simulated developer with no platform knowledge. Confirmed `.woodpecker.yml`
   was already present — never authored by "the developer."
3. Added `requirements.txt` pinning `pyyaml==5.3.1` — a real CVE (CVE-2020-14343, arbitrary code
   execution via `yaml.load()` without a safe loader) — and a small `app.py` using it. Committed,
   pushed to a feature branch, opened a PR. No scanner invoked manually, no Woodpecker UI touched.

## Five real bugs found and fixed along the way

Reporting all of them, including the ones that were false starts, because each is a genuine
operational finding, not noise:

### 1. Windows bind-mount filesystem corruption (infrastructure, not this test specifically)

Mid-session, Postgres crashed and recovered, then hit `could not open file ... Permission denied`
on a live query. Root cause: bind-mounting Postgres's data directory through the Windows
NTFS→WSL2→Docker boundary. **Fix:** switched `postgres`, `gitea`, and `woodpecker-server` to
Docker-managed named volumes instead of `./data/...` host bind mounts. This is a real risk on any
Windows host running this profile, not specific to this machine — documented directly in
`compose/minimal/docker-compose.yml`.

### 2. Branch protection blocked the platform's own automation

`onboard-repo.sh`'s first version set `enable_push: false` after committing the pipeline template —
correct for developers, but it meant *re-running the script later* (e.g., to push an updated
template) 403'd on its own commit, exactly as hard as it blocks a direct developer push. **Fix:** a
`push_whitelist` for the automation account itself, `enable_push: true` + `enable_push_whitelist:
true` + `push_whitelist_usernames: [<automation account>]` — Gitea's actual model, also learned the
hard way (a first attempt set `enable_push_whitelist: true` while leaving `enable_push: false`,
which the API silently accepted and then ignored: `enable_push=false` means no one can push, full
stop, whitelist or not).

**Security tradeoff, stated plainly:** this gives whoever holds the platform's Gitea admin token a
standing bypass of PR review on every onboarded repo. That token must be scoped and protected
accordingly — the same concern D2 raises about auditing broad-scope PATs, now applied to the
platform's own identity. Acceptable while this script *is* the platform admin's tool; revisit before
any real developer team is onboarded.

### 3. `network_mode: service:gitea` fixed the OAuth path but not the clone path

SADR-0004's fix made Woodpecker's OAuth/API calls work by sharing Gitea's network namespace. It did
**not** fix pipeline execution: the agent spins up a **separate, ephemeral container per step**, on
Woodpecker's own auto-created pipeline network, which shares nothing with Gitea. The clone step
failed: `fatal: unable to access 'http://127.0.0.1:3500/...': Could not connect to server` — because
`127.0.0.1` is only ever self-referential; no network attachment changes what it means. **Fix:**
`WOODPECKER_BACKEND_DOCKER_NETWORK=minimal_default` on the agent, attaching every step container to
the same compose network Gitea is actually on.

### 4. `ROOT_URL=127.0.0.1` broke step-container clones even with the network fix

Attaching step containers to the right network wasn't sufficient on its own: Gitea embeds its own
`ROOT_URL` into every `clone_url` and webhook payload it generates, and that was still
`http://127.0.0.1:3500/` — unreachable from any container, network attachment or not, for the same
reason as #3. **Fix, the actually-correct one:** `GITEA__server__ROOT_URL=http://gitea:3500/` — the
container's own service name, resolvable via Docker's embedded DNS both from step containers (same
compose network) and from `woodpecker-server` (shares Gitea's netns, so this is a self-lookup). This
is precisely the "put both services behind one consistent hostname" fix [SADR-0004](0004-woodpecker-live-spike.md)
recommended from the start, before the `127.0.0.1` shortcut was tried first because it was simpler.
It solved the OAuth path well enough to look complete; it was not complete. A real deployment
resolves this permanently with a reverse proxy in front of both services — tracked, not built yet.

### 5. `trivy --skip-db-update` cannot run on a container with no prior database

The pipeline template's first version passed `--skip-db-update`, matching DESIGN.md's target design
(a persisted, out-of-band-refreshed Trivy DB cache). That infrastructure doesn't exist in this
profile yet, and every step container here **is** a first run — freshly created per pipeline, no
cache. Trivy failed outright: `--skip-db-update cannot be specified on the first run`. **Fix:**
removed the flag for now; Trivy downloads its ~109 MB database fresh every run until a shared,
persisted cache exists (a real, tracked follow-up, not silently deferred).

**Also hit again:** the same unquoted-colon YAML gotcha from the earlier Semgrep pipeline test
(`docs/adr/0004`'s "fix the yaml quoting" moment) — this time in the template's `summary` step. Fixed
by wrapping every command containing a colon in single-quoted YAML scalars, with the reason recorded
directly in the template so it doesn't get relearned a third time.

## Result

```
onboard-repo.sh gateadmin sca-dast-test
  → [1/3] activated  [2/3] .woodpecker.yml committed  [3/3] protection set (push whitelist: gateadmin)
  → clean run, first try, zero manual follow-up

git clone / add pyyaml==5.3.1 / push feature branch / open PR
  → pipeline auto-triggered, no scanner invoked by hand

clone       → success
secrets     → success  (0 findings)
sast        → success  (0 findings)
dependencies→ FAILURE  (exit 1)
summary     → success  → "dependency findings: 1"

GET /repos/gateadmin/sca-dast-test/commits/<sha>/status
  combined state: failure, total_count: 2
    ssdlc/security-gate/push/woodpecker → failure  "Pipeline failed"
    ssdlc/security-gate/pr/woodpecker   → failure  "Pipeline failed"
```

**Confirmed, completely.** Trivy caught a real, named CVE (CVE-2020-14343) in a dependency a
simulated developer added with no awareness of the scanning pipeline. Both the `push`-triggered and
`pr`-triggered pipelines produced failing, glob-matched commit statuses, and the PR is genuinely
blocked by branch protection — not merely "a scan ran," but "the platform's own gate mechanism
blocked this exact PR for this exact reason."

**One further correction to DESIGN.md D1's context-string note:** the PR event's context is
`ssdlc/security-gate/pr/woodpecker` — Woodpecker's actual event name is `pr`, not `pull_request` as
D1's note assumed. The glob pattern (`ssdlc/security-gate/**`) already used in `onboard-repo.sh`
absorbs this correctly regardless; only the illustrative prose needs the fix.

## Decision

1. `pipelines/fast.woodpecker.yml` and `scripts/onboard-repo.sh` are the first two pieces of real,
   working paved-road automation, not just designed — carry both forward as-is into Milestone 2's
   scope, extending rather than replacing them.
2. Persist a shared Trivy DB volume and a scheduled out-of-band refresh job before re-adding
   `--skip-db-update` — tracked, not done. Every run currently re-downloads ~109 MB, which is correct
   but slow, and will not stay under the fast gate's 3-minute budget at any real scale.
3. `onboard-repo.sh`'s push-whitelist tradeoff (item 2 above) needs a real answer before any actual
   developer team is onboarded — likely resolved once Milestone 3's sidecar exists and the automation
   account's token can be scoped far more narrowly than a full Gitea admin token.
4. `WOODPECKER_BACKEND_DOCKER_NETWORK` and Gitea's service-name `ROOT_URL` are both now permanent,
   documented parts of `compose/minimal/docker-compose.yml` — required for this profile to function
   at all, not optional hardening.
5. This experiment's methodology — onboard, clone, code with a real vulnerability, push, verify the
   gate — becomes the template for the still-outstanding DAST leg (Juice Shop + ZAP) and for the
   eventual automated end-to-end regression test named in DESIGN.md's Verification section.

## Reproduce it yourself

```
cd compose/minimal
cp .env.example .env   # POSTGRES_PASSWORD, WOODPECKER_AGENT_SECRET, WOODPECKER_GRPC_SECRET
docker compose up -d postgres
# wait healthy, then:
docker compose up -d gitea
# wait http://127.0.0.1:3500/api/healthz == "pass", create admin user, mint a token,
# register an OAuth2 app (redirect_uri http://127.0.0.1:8000/authorize), put creds in .env
docker compose up -d woodpecker-server
# wait healthy, then:
docker compose up -d woodpecker-agent
# log in (see docs/adr/0004's method), export GITEA_URL/WOODPECKER_URL/GITEA_ADMIN_TOKEN/
# WOODPECKER_SESSION/WOODPECKER_CSRF, then:
../../scripts/onboard-repo.sh <owner> <repo>
# clone, add a requirements.txt with a known-CVE package, push to a feature branch, open a PR
docker compose down -v
```
