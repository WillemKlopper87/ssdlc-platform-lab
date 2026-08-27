# SADR-0021: Persistent, shared Trivy DB cache

**Status:** Accepted, implemented.

**Date:** 2026-08-27
**Milestone:** 3 -- fast-gate/pipeline debt
**Security & Privacy Impact:** Low (performance/availability, not a trust-boundary change)

## Question

`pipelines/fast.woodpecker.yml`'s own header comment documented the gap directly: every
`dependencies` step re-downloads Trivy's ~109MB vulnerability DB from scratch, because each step
runs in a freshly-created, throwaway container with no persistent cache. `docs/TODO.md` tracked
this as real fast-gate debt -- it will not stay under the 3-minute budget at real scale, and every
run pays a real network cost for no reason once a DB has already been fetched once.

## Decision

**A Terraform-managed named volume** (`ssdlc-<profile>-trivy-db-cache`, `terraform/local/main.tf`,
matching the existing pattern for `postgres-data`/`gitea-data`/`woodpecker-server-data`), mounted
into **every** pipeline step container on the agent via `WOODPECKER_BACKEND_DOCKER_VOLUMES` in
`compose/minimal/docker-compose.yml`.

**Confirmed against Woodpecker's own source** (`pipeline/backend/docker/docker.go`,
`pipeline/backend/docker/config.go`, matching v3.17.0, the pinned version) rather than assumed:
`WOODPECKER_BACKEND_DOCKER_VOLUMES` is agent-level configuration, parsed once at agent startup and
appended to every container's host config unconditionally
(`hostConfig.Binds = utils.DeduplicateStrings(append(hostConfig.Binds, e.config.volumes...))`) --
not opt-in per pipeline, not something a step's own YAML can add to or influence. This matters for
the same reason the Gate Contract's protected-base comparison matters (SADR-0017): a mechanism a PR
cannot touch is a stronger guarantee than one that merely happens to work today.

Mounted at Trivy's own default cache path, confirmed via `trivy --help` rather than assumed:
`/root/.cache/trivy`. No `--cache-dir` override needed in the pipeline.

**The `dependencies` step self-heals** rather than assuming a separate job has already primed the
volume:

```sh
test -f /root/.cache/trivy/db/metadata.json && SKIP_DB="--skip-db-update" || SKIP_DB=""
trivy fs $SKIP_DB --exit-code=0 --format=json --output=trivy-report.json --skip-files=... .
```

An empty cache (a fresh volume, or the first run after this change ships) still works correctly --
one real download primes it, same as before this change, just once instead of every run from then
on.

**A scheduled refresh job**, `pipelines/trivy-db-refresh.woodpecker.yml`, matches
`dast-scheduled.woodpecker.yml`'s established cron pattern and DESIGN.md's own stated intent: "CI
runs `--skip-db-update` so a bad upstream DB cannot break every build at once" implies the *fast
gate* should never touch the network for this, and something else, on a predictable schedule,
should. Not yet registered as an actual Woodpecker cron job -- that is a manual registration step
against a running instance, the same gap SADR-0016 left for `self-verify.woodpecker.yml`'s own cron
registration.

## Verification

- Isolated: primed an empty throwaway volume via `trivy image --download-db-only` directly (a real
  109MB download, confirmed via the volume's own `metadata.json` after); confirmed a subsequent
  `trivy fs --skip-db-update` scan against the same volume succeeds (exit 0, valid JSON output, no
  "cannot skip downloading DB" error).
- Live, end to end: onboarded a throwaway test repo with the updated pipeline, `terraform apply`'d
  the new volume, recreated the Woodpecker agent to pick up the new mount config. Pushed a commit
  with a real vulnerable dependency (`pyyaml==5.3.1`) twice, on two separate pipeline runs (one
  `push`-event, one `pull_request`-event). Fetched both runs' `dependencies` step logs via
  Woodpecker's actual log API (`GET /api/repos/{id}/logs/{pipeline}/{step_id}` -- confirmed the
  correct route from Woodpecker's own router source; several nearby-looking paths return the web
  UI's SPA shell instead of log data and silently look like success). Both logs show zero DB-download
  network activity -- the cache was already primed by the time these two runs happened (confirmed
  directly by reading the volume's own `metadata.json`, `DownloadedAt` predating both pipeline
  start times), and both correctly used `--skip-db-update` with no error. `policy-eval-findings`
  correctly blocked the real vulnerable dependency in both runs, confirming the cache change did not
  silently break detection.
- Full `tests/unit/` tier re-run clean afterward.

## Consequences

- Every onboarded repo's `dependencies` step benefits automatically -- no per-repo configuration,
  since the mount is agent-level.
- The scheduled refresh job exists as a file but needs manual cron registration before it does
  anything -- tracked, not silently assumed wired up.
- `WOODPECKER_BACKEND_DOCKER_VOLUMES` now mounts this cache into **every** step of **every**
  pipeline on this agent, not just `dependencies` steps -- harmless (unused by any step that doesn't
  reference the path), but worth knowing if a future step's own tooling happens to write to
  `/root/.cache/trivy` for an unrelated reason.
