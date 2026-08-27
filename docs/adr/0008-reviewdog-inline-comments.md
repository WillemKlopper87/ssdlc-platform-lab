# SADR-0008: Milestone 2 opened — reviewdog inline PR comments, live-tested, real comments posted

**Status:** Confirmed by experiment
**Date:** 2026-08-24
**Milestone:** 2 (Fast gate, comments only) — first concrete artifact
**Security & Privacy Impact:** Low. Adds a reporting mechanism; does not change any gate decision —
`sast`/`dependencies`/`secrets` remain the actual (separate) blocking steps, unchanged by this work.

## Question

DESIGN.md's roadmap names Milestone 2 plainly: "Gitleaks + Semgrep + Trivy + normalise adapters.
reviewdog inline comments. **No blocking** — measure the false-positive rate first." This experiment
builds and live-tests the reviewdog half specifically: can Semgrep's findings reach a Gitea PR as
real, file-and-line inline comments, with reviewdog's native Gitea reporter, and with no human
posting anything by hand?

## Method

Extended `pipelines/fast.woodpecker.yml` with a new `inline-comments` step, installing reviewdog
(`v0.21.0`, matching PINNED_VERSIONS.md) via its official install script and running it with the
`gitea-pr-review` reporter against Semgrep's SARIF output. Reused the `compose/minimal` stack and the
established live-login method (SADR-0004). Pushed a small Python fixture with two real, distinct
vulnerabilities (`subprocess.call(..., shell=True)`, `pickle.loads()`) — the same pattern already
proven to trigger real Semgrep findings in this session's earlier standalone demo — and opened a PR.

Before building, verified two things from reviewdog's own source rather than assuming them:

- **The Gitea reporter's real requirements**: `-reporter=gitea-pr-review`, with
  `REVIEWDOG_GITEA_API_TOKEN` and `GITEA_ADDRESS` env vars (`cmd/reviewdog/main.go`).
- **reviewdog's actual built-in input formats**: `checkstyle`, `rdjson`, `rdjsonl`, `sarif`
  (`parser/` directory listing) — there is **no** dedicated `semgrep` format, despite at least one
  search result implying otherwise. Used `-f=sarif` against Semgrep's native `--sarif` output
  instead of trusting the unverified claim.

## Six real bugs found and fixed, in the order encountered

### 1. Woodpecker skips a step by default once an earlier step fails — `inline-comments` needs the same `status: [success, failure]` the `summary` step already has

Missed on the first version of the file. Findings exist precisely when `sast` fails — that is the
one condition under which `inline-comments` must run, and the one condition under which the default
Woodpecker behaviour skips it. Caught live: the first PR that `sast` correctly failed against showed
`inline-comments: skipped`.

### 2. Gitea derives `clone_url` (and the webhook payload built from it) from the *triggering request's* `Host` header, not solely from static `ROOT_URL`

The clone step failed outright: `fatal: unable to access 'http://127.0.0.1:3500/...': Could not
connect to server`. `GITEA__server__ROOT_URL=http://gitea:3500/` was correctly configured (verified
directly in `docker-compose.yml`) — yet `GET /api/v1/repos/.../` returned `clone_url:
http://127.0.0.1:3500/...`. Confirmed the mechanism directly: the same request, with an explicit
`Host: gitea:3500` header added, returned `clone_url: http://gitea:3500/...` instead. Almost
certainly interacting with the `REVERSE_PROXY_TRUSTED_PROXIES=127.0.0.1/32` setting D6 added
deliberately (trusting `127.0.0.1` as a legitimate proxy hop, then deferring to that hop's own
`Host` header) — every host-side API/git call in this session had been made via `127.0.0.1`, which
is exactly the address now trusted to say what the "real" hostname is. **Fix:** add an explicit
`Host: gitea:3500` header (`-H` for curl, `-c http.extraHeader=` for git) to any host-side action
that triggers a push or PR event — the events that generate webhook payloads Woodpecker must resolve
from inside its own containers. This affects every future live test in this profile equally, not
just this one; worth lifting into a documented helper once real tooling exists.

### 3. The `apk add curl` / reviewdog-install commands' output was suppressed with `> /dev/null 2>&1`, which hid a real early failure with no diagnostic content

The step failed with a single truncated log line and no visible error. Removing the suppression
didn't just reveal the cause — the very next run succeeded outright, consistent with transient
package-mirror flakiness that would have been invisible and unexplainable with the output
suppressed. **Decision: never suppress output on a command whose failure would otherwise be silent.**
Quiet success is fine to keep quiet; quiet failure is not.

### 4. Semgrep's `--output` flag cannot be specified twice in one invocation, even across different `--format` flags

`--json --output=semgrep-report.json --sarif --output=semgrep-report.sarif` in one command failed
outright: `semgrep scan: option '--output' cannot be repeated`. **Fix:** two separate Semgrep
invocations, one per format.

### 5. Command order within the fixed step mattered, and the first fix got it backwards

Woodpecker stops a step's command list at the first failing command. Placing the `--error --json`
run (the real gate signal, meant to fail the step on findings) *before* the `--sarif` run meant the
SARIF file was never written on exactly the runs where it mattered — the failing ones, which are the
only ones with anything for reviewdog to report. **Fix:** SARIF generation first, with its own exit
code swallowed (`|| true`); the `--error --json` gate run last, so it alone governs the step's
overall pass/fail.

### 6. None of the above actually blocked reviewdog once fixed — the mechanism worked on the very next attempt

Once bugs 1–5 were resolved, the pipeline produced a real SARIF file with both fixture findings, and
reviewdog consumed it successfully without further changes.

## Result

```
GET /repos/gateadmin/reviewdog-test/pulls/2/reviews
  -> 1 review: reviewer=gateadmin, state=COMMENT, comments=2

GET .../reviews/{id}/comments
  -> file=app.py: "🚫 [Semgrep OSS] <python.lang...subprocess-shell-true> ..."
  -> file=app.py: "⚠️ [Semgrep OSS] <python.lang...avoid-pickle> ..."
```

**Confirmed.** Two real inline PR comments, correctly attributed to `app.py`, each linking back to
its Semgrep rule page, each carrying reviewdog's own severity iconography — posted with zero human
interaction beyond pushing the vulnerable code and opening the PR. The `sast` step itself still
failed the pipeline as designed (unchanged gate behaviour); `inline-comments` is a genuinely
*additional* signal, exactly matching Milestone 2's "comments only, no new blocking" scope.

## Decision

1. **Milestone 2 is open, with its first real artifact.** `pipelines/fast.woodpecker.yml` now has a
   working, live-tested inline-comment mechanism — carry forward as-is.
2. **The `Host: gitea:3500` requirement (bug 2) is now a standing operational fact** for every
   future live test against this profile, not specific to reviewdog. Add it explicitly to
   `docs/OPERATIONS.md` once that document exists, and consider whether `onboard-repo.sh` or a
   shared test-helper script should set it automatically rather than relying on every future session
   remembering it by hand.
3. **Milestone 2's actual measurement goal — the false-positive rate — has not started.** This SADR
   proves the *mechanism*; it does not yet constitute the observation period DESIGN.md calls for
   before anything enforces on reviewdog's output. That period starts once this pipeline runs against
   real, non-fixture code.
4. Extend `inline-comments` to also consume Trivy's and Gitleaks's output once each has (or can be
   given) SARIF output — not done here, scoped narrowly to Semgrep for this first proof.

## Reproduce it yourself

```
cd compose/minimal
cp .env.example .env
docker compose up -d postgres
# wait healthy, then:
docker compose up -d gitea
# wait http://127.0.0.1:3500/api/healthz == "pass", create admin, mint a token,
# register an OAuth2 app (redirect_uri http://127.0.0.1:8000/authorize)
docker compose up -d woodpecker-server woodpecker-agent
# log in (docs/adr/0004's method), activate a repo, commit pipelines/fast.woodpecker.yml
# create a Woodpecker secret "reviewdog_gitea_token" (a real Gitea token, write:issue scope)
# push a fixture with a real vulnerability, using -H/-c http.extraHeader "Host: gitea:3500"
#   on every git/curl call that triggers a push or PR event (see bug 2 above)
# open a PR the same way; check GET .../pulls/{n}/reviews for a real posted review
docker compose down -v
```
