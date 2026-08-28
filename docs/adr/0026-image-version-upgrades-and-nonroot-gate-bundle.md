# SADR-0026: Self-scan-driven image upgrades — Postgres 16→18, Woodpecker 3.17→3.18, non-root gate bundle

**Status:** Accepted, implemented, live-verified.

**Date:** 2026-08-28
**Milestone:** 3 — dogfooding (D6): the platform's own scanners, run against its own repo and its own
deployed images
**Security & Privacy Impact:** Medium. Closes real, upstream-confirmed CVE exposure in two deployed
images and a real container-hardening gap in the trusted-runner bundle; touches no application-level
gate logic.

## Question

DESIGN.md's D6 names dogfooding explicitly: this platform's own Compose/IaC should pass its own gates.
That had never actually been done end to end against the deployed stack's own container images. Run
the platform's real scanners — Gitleaks, Semgrep (vendored ruleset), Trivy fs, Trivy config, Trivy
image — against the project's own repo and its own deployed images, then act on what a real,
independently-corroborated finding turns out to be.

## Method

Live scan against a clean `git archive HEAD` export (not the working tree, to avoid conflating local
scratch/`.env` noise with the committed project — and not the raw working tree either, since
`policy/vendored-rules/`'s own ~700 rule-test fixtures self-contaminate every scanner that includes them,
the same class of bug SADR-0018/0024/0025 already found and fixed elsewhere; excluded here the same
way). Findings run through the real `policy-eval/evaluate-findings.py` for an authoritative
severity-classified verdict, not just raw scanner output.

**Result:** secrets and dependencies clean (zero real findings — the only Gitleaks hits are two
deliberately fake-secret test fixtures; there are no dependency manifests to scan). Two real,
independently-corroborated findings:

1. **`gate-bundle/Dockerfile` never leaves `USER root`** — flagged by both Semgrep
   (`rules.dockerfile.last-user-is-root`) and Trivy config (`DS-0002`, HIGH). This image's whole job is
   executing scans against untrusted PR content (SADR-0017's trusted-runner design) — running as root
   inside it is a real gap on top of `run-container.sh`'s existing `--cap-drop ALL --network none
   --read-only` isolation, not redundant with it.
2. **Deployed image CVE exposure**, via `trivy image --severity=CRITICAL,HIGH` against every image this
   profile actually runs: `postgres:16-alpine` (24 findings, 1 Critical — a Go-stdlib CVE in the
   bundled `gosu` binary), `gitea/gitea:1.27.2` (15), `woodpecker-server:v3.17.0` (10),
   `woodpecker-agent:v3.17.0` (8) — dominated by Go-stdlib CVEs from each binary's own build-time Go
   toolchain, plus Gitea-specific module CVEs (`go-git`, `golang.org/x/mod`, `golang.org/x/text`,
   `grpc`). `bkimminich/juice-shop` deliberately excluded — it's the intentionally-vulnerable DAST
   target (SADR-0006), not a platform gap.

Checked what was actually actionable, not just what was flagged, against each project's real GitHub
Releases: **Gitea `1.27.2` is already the latest release** — nothing to bump, the module CVEs found are
upstream-unpatched as of this scan. **Woodpecker `v3.18.0`** was newer than the pinned `v3.17.0`.
**`postgres:16-alpine` was itself a floating tag** — `docs/PINNED_VERSIONS.md`'s own rule
("never a floating tag") had been violated by this exact pin since it was first written; the
locally-cached copy was seven weeks stale regardless of the tag's own mutability.

## Decision

1. **`gate-bundle/Dockerfile`**: reuse the `semgrep` base image's own existing non-root user (UID 1000,
   already present in `semgrep/semgrep:1.174.0` — not invented), `chown -R` the files `COPY`'d in as
   root, `USER semgrep` before `ENTRYPOINT`.
2. **Woodpecker `v3.17.0` → `v3.18.0`**, server and agent kept in lockstep (the release enforces
   stricter gRPC protocol-version matching between them). Confirmed nothing in this repo uses the
   release's one renamed env var (`WOODPECKER_GRPC_VERIFY` → `WOODPECKER_GRPC_SKIP_VERIFY`).
3. **`postgres:16-alpine` → `postgres:18.6-alpine`**, pinned to the exact patch version this time, not
   a floating major-minor tag. A **major** version jump, not a patch bump — PostgreSQL's on-disk
   format is not compatible across major versions, so this needed a real migration, not an image swap.
4. **Gitea**: left at `1.27.2` — already the latest release, confirmed live against the GitHub
   Releases API, not assumed.

## The Postgres migration, live

No `pg_upgrade` ships in the official image for this scale of deployment; the documented, standard
path for a Compose-based setup this small is a logical dump/restore:

1. `pg_dumpall -U ssdlc` against the live 16-instance — verified non-trivial (22 MB, all three real
   databases present, 135 tables, both Gitea's `repository` and Woodpecker's `pipelines` tables
   confirmed present) **before** anything destructive.
2. `docker compose down`, then `docker volume rm ssdlc-minimal-postgres-data` — the one irreversible
   step, taken only with that verified backup as the safety net.
3. `terraform apply` to recreate the (Terraform-managed, SADR-0014) volume from its own original
   config, not an improvised `docker volume create`.

**Found live, not anticipated:** Postgres 18's official image made a real, documented breaking change
to its own expected mount layout (`docker-library/postgres#1259`) — 18+ wants a single mount at
`/var/lib/postgresql` and creates its own major-version-named subdirectory inside it
(`pg_ctlcluster`-style), specifically to support `pg_upgrade --link` across versions without a
mount-point boundary in the way. The pre-18 convention this profile used (`postgres-data:/var/lib
/postgresql/data`) makes the container refuse to start outright, with the image's own entrypoint
explaining exactly why. Fixed in `compose/minimal/docker-compose.yml`: mount moved up one level, volume
name unchanged.

4. Fresh cluster initialized; `init-multi-db.sh` pre-created empty `gitea`/`woodpecker` databases as it
   always does on first boot.
5. Restored the dump via `psql` (not `pg_restore` — `pg_dumpall` output is plain SQL). The dump's own
   `CREATE DATABASE`/`CREATE ROLE` statements for `gitea`/`woodpecker`/`ssdlc` correctly errored as
   "already exists" (`psql` continues past errors by default, absent `-v ON_ERROR_STOP=1`) — harmless,
   since the dump's subsequent `\connect` + schema/data statements populate those same
   already-existing, still-empty databases regardless.

## Verification

Live, end to end, not just "the containers started":

- `gate-bundle` image built and its entrypoint confirmed running as `uid=1000(semgrep)`, not root.
- `gitea.repository`: 2 rows. `woodpecker.pipelines`: 111 rows. Both match pre-migration state.
- Full stack brought up on the new images; Gitea API responds (`{"version":"1.27.2"}`), Woodpecker
  responds (HTTP 200), and **both pre-existing repositories are visible again through the live Gitea
  API** (`gateadmin/gate-bypass-07a`, `ssdlc-admin/pilot-app`) — not just that the migration completed,
  but that the platform actually works against the migrated data.
- One transient event during this work, unrelated to the migration itself and left in the record
  rather than smoothed over: the freshly-migrated Postgres container received a clean shutdown
  (checkpoints completed normally, exit 0 — not a crash) partway through verification, consistent with
  this shared machine's already-documented recurring Docker Desktop instability. `docker start`
  recovered it with no further action needed.

## Consequences

- Closes the one Critical and most of the High findings this scan found in `postgres`'s bundled
  `gosu` binary. The remaining Go-stdlib and Gitea-module CVEs in `gitea`/`woodpecker-server`/
  `woodpecker-agent` are **not fixable by re-pinning** — no newer release exists yet for any of them as
  of this scan. Tracked, not silently dropped: re-run this exact scan at the next milestone boundary
  per `PINNED_VERSIONS.md`'s own re-verification cadence, and re-pin the moment a fixed release ships.
- `docs/PINNED_VERSIONS.md`'s Core platform table updated with the new versions and this scan's date.
- A reminder matching this project's own recurring pattern: **a "pinned" version can still be a
  floating tag in practice** (`16-alpine`, not `16.9-alpine`) — the discipline `PINNED_VERSIONS.md`
  states was violated by one of its own oldest entries until this SADR:  worth an audit pass across
  the rest of the table, not assumed fixed just because this one instance was caught.
- The Postgres 18 mount-layout change is exactly the kind of "found live, not in a changelog skim"
  fact this project's SADRs exist to record — a future upgrade past 18 should check
  `docker-library/postgres`'s own release notes for the next such structural change before assuming a
  bare tag bump is safe.
