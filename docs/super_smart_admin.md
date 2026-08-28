# super_smart_admin.md

The platform-admin guide: deploying this thing, standing it up end to end, managing accounts, keeping
it running, and knowing exactly what's real versus what's still a plan. Written for the person who runs
`SSDLC-Platform-Lab`, not the developers using it — see
[`DEVELOPER_HOWTO.md`](DEVELOPER_HOWTO.md) for that audience.

**Status discipline, up front:** every claim below is checked against the actual code and config in
this repo as of this writing, not against `DESIGN.md`'s target architecture. Where something is
designed but not built, this document says so explicitly — the project's own `ARCHITECTURE.md` and
`docs/TODO.md` are the living source of truth if this page drifts; re-check against them.

---

## 1. What you're actually running

Gitea (the Git server/forge) + Woodpecker CI + Postgres, deployed via Terraform (network/volumes) +
Docker Compose (`minimal` profile). A Woodpecker pipeline template gets committed into every onboarded
repository; it runs Gitleaks, Semgrep, and Trivy, then a Python evaluator (`policy-eval/`) makes the
actual block/pass decision against one reviewable Rego policy file. A separate standing process
(`scripts/bot-approver.py`) casts the platform's own required approval once scanning is clean and the
pipeline/policy files themselves haven't been tampered with.

This is a **pilot**, not a hardened multi-tenant production service. Read
[`docs/what_next.md`](what_next.md) for the honest gap list before promising anything to a real team.

## 2. Deploying it, start to finish

### 2.1 Prerequisites

- Docker Desktop (or an equivalent Docker + Compose v2 install)
- Terraform
- A POSIX shell (`scripts/*.sh` are `sh`, not `bash`-specific, but native Windows `cmd`/PowerShell
  cannot run them directly — use Git Bash or WSL2 on Windows)

### 2.2 First-time setup

```sh
cd SSDLC-Platform-Lab
./scripts/quickstart.sh
```

The first run will refuse to continue and instead:

1. Copy `compose/minimal/.env.example` to `compose/minimal/.env`.
2. Tell you to fill in the placeholders and register the Gitea OAuth application, then re-run.

**Fill in `.env`:**

| Variable | How to get it |
|---|---|
| `POSTGRES_PASSWORD` | Any strong password — generate one, don't leave `change-me` |
| `WOODPECKER_AGENT_SECRET` | `openssl rand -hex 32` |
| `WOODPECKER_GRPC_SECRET` | `openssl rand -hex 32` — **must be set explicitly**, not left to auto-generate (Woodpecker regenerates it on every restart otherwise, silently invalidating every session — SADR-0004) |
| `GITEA_OAUTH_CLIENT_ID` / `GITEA_OAUTH_CLIENT_SECRET` | From the OAuth app registration below |
| `WOODPECKER_PORT` | Only change if `8000` is already taken on your host |

**Register the Gitea OAuth application** (you need Gitea running for this — start just that piece
first if you haven't run `quickstart.sh` yet, or run the full stack once with placeholder OAuth values
and fix them after):

1. Log into Gitea as an admin (`http://127.0.0.1:3500`).
2. Site Administration → Applications → Manage OAuth2 Applications → Create a new OAuth2 Application.
3. Redirect URI: `http://127.0.0.1:<WOODPECKER_PORT>/authorize` (matching whatever port you set above —
   if you change `WOODPECKER_PORT` later, update this redirect URI too, or logins will silently break).
4. Copy the generated Client ID and Client Secret into `.env`.

Then run `./scripts/quickstart.sh` again. It will:

1. Run `scripts/doctor.sh` (read-only preflight — checks Docker's actually reachable, `.env` has no
   leftover placeholders, `docker compose config` validates).
2. `terraform -chdir=terraform/local apply` — provisions the external Docker network and named volumes.
   **Never substitute a bare `docker compose up` for this** — Compose must not be allowed to silently
   create its own copies of Terraform-owned state.
3. `docker compose up -d` — brings up Postgres, Gitea, Woodpecker server + agent, and the DAST staging
   target.

Endpoints: Gitea `http://127.0.0.1:3500`, Woodpecker `http://127.0.0.1:${WOODPECKER_PORT}` (default
`8000`).

### 2.3 Mint the durable Woodpecker API token

Every automation script (`onboard-repo.sh`, `bot-approver.py`) needs a **durable** Woodpecker personal
access token. This is a one-time interactive step — **do not** try to read the CSRF secret out of the
database or script around it (OPERATIONS.md's own hard-learned rule, SADR-0004/0011):

1. Log into Woodpecker in a browser (`http://127.0.0.1:${WOODPECKER_PORT}`), authenticated via the
   Gitea OAuth app above.
2. `GET /web-config.js` with your session cookie gives you a legitimately-issued CSRF token.
3. `POST /api/user/token` once, using that CSRF token, to get a durable PAT.
4. Save it as `WOODPECKER_TOKEN` wherever `onboard-repo.sh` / `bot-approver.py` will read it from.

### 2.4 Restarting after a crash / on a new day

`docker compose up -d` in `compose/minimal/` is normally enough if Terraform-managed state already
exists. This environment has had recurring Docker Desktop instability — if containers are missing or
crash-looping, that's the usual cause; bringing the stack back up with `docker compose up -d` resolves
it in practice, not a code bug to chase.

## 3. Accounts and identity

**There is no self-registration.** `GITEA__service__DISABLE_REGISTRATION=true` is set deliberately in
`compose/minimal/docker-compose.yml`. Every account — yours, the bot's, every developer's — is created
by you, the admin, via the CLI:

```sh
docker exec -u git ssdlc-minimal-gitea gitea admin user create \
  --username <name> --password '<temp-password>' --email <email> [--admin] [--must-change-password]
```

Drop `--admin` for ordinary developer and bot accounts — only the platform-admin account itself needs
it. There is no self-serve login/signup page anywhere in this stack today; developers log in through
Gitea's own native login screen using an account you provisioned first. (The design-preview portal
covered in `DEVELOPER_HOWTO.md` doesn't build a login flow either — one of the two alternate portal
directions explored in the design canvas sketches one, but it isn't real.)

### 3.1 The accounts you actually need

| Account | Purpose | Token scope |
|---|---|---|
| Platform admin (e.g. `gateadmin`) | Runs `onboard-repo.sh`, holds `GITEA_ADMIN_TOKEN` | `write:repository`, `write:admin` — this is a standing PR-review bypass on every onboarded repo (branch protection's push-whitelist has to include it so onboarding doesn't lock itself out). Scope and protect accordingly. |
| Gate bot (e.g. `gate-bot`) | Casts the platform's required approval — `scripts/bot-approver.py`'s identity | `write:repository` only, on its own dedicated account. This is D2's "crown jewel" credential — it lives only wherever `bot-approver.py` runs, **never** inside a build container. |
| Reconciliation identity | Pushes the real empty-commit "nudge" for a stuck PR — `scripts/reconciliation-loop.py`'s identity | `write:repository` only, its own separate account/token — deliberately not the bot's or the admin's. |
| Policy-eval reader | `approval-check`'s read-only re-derivation of approval validity, runs inside the pipeline | Read-only (`read:repository`, `read:issue`, `read:organization` if team-based whitelisting is used) **but on an account with admin permission on the repo** — reading `branch_protections` requires repo-admin, a plain collaborator gets 403 |
| Each developer | Normal Gitea usage | Whatever you'd normally grant a contributor |

Generate a token for any of these via CLI too:

```sh
docker exec -u git ssdlc-minimal-gitea gitea admin user generate-access-token \
  --username <name> --token-name <label> --scopes write:repository,write:admin
```

## 4. Onboarding a repository

**Before you run this**, `scripts/bot-approver.py` must already be running against the repo (or start
immediately after) — onboarding sets `required_approvals: 2` on branch protection, and if nothing is
casting the bot's half, every PR on that repo becomes permanently unmergeable, not safer.

```sh
export GITEA_URL=http://127.0.0.1:3500
export WOODPECKER_URL=http://127.0.0.1:8000
export GITEA_ADMIN_TOKEN=<platform admin token>
export WOODPECKER_TOKEN=<durable Woodpecker PAT from 2.3>
./scripts/onboard-repo.sh <owner> <repo>
```

This is idempotent — re-running it against an already-onboarded repo is safe and treats "already
active" / "already up to date" as success, not an error. In order, it:

1. Looks up the repo's Gitea-internal ID and activates it in Woodpecker.
2. Generates the differential-gating baseline (a scan of the repo's *current* state, before any
   platform file lands — SADR-0024/0025) and writes `.ssdlc/baseline.json`. A re-run refreshes
   (shrinks) the existing baseline rather than starting over.
3. Commits the pipeline template and everything it depends on at runtime — `.woodpecker.yml`,
   `policy-eval/`, `normalise/`, `policy/severity.rego`, and the full `policy/vendored-rules/` tree.
4. Adds the gate bot as a write collaborator.
5. Sets branch protection: the fast gate's status context (glob-matched), `required_approvals: 2`,
   `dismiss_stale_approvals: true`, push-whitelist for your own automation account (so future
   `onboard-repo.sh` re-runs against this repo don't lock themselves out).

**Verify it actually worked:** open a real PR against the onboarded repo and confirm, in order — the
pipeline runs and reports a status; `bot-approver.py`'s own logs show it evaluating the PR (not
silently skipping on a gate-contract mismatch); the bot's approval lands once scanning is green; a
human approval plus the bot's together satisfy `required_approvals: 2`.

## 5. Running the standing services

Neither of these is a `compose/minimal` service yet — both are scripts you start by hand, and both are
currently single-repository per process (tracked as a real gap in `docs/TODO.md`).

**`scripts/bot-approver.py`** — polls, checks scanning steps + gate-contract match, casts the bot's
approval:

```sh
export GITEA_URL=http://127.0.0.1:3500
export GITEA_BOT_TOKEN=<gate-bot's own token>
export WOODPECKER_URL=http://127.0.0.1:8000
export WOODPECKER_TOKEN=<durable Woodpecker PAT>
export REPO_OWNER=<owner> REPO_NAME=<repo> WOODPECKER_REPO_ID=<id>
export GITEA_BOT_LOGIN=gate-bot
export GATE_CONTRACT_ENFORCE=1   # the secure default; only ever 0 for a deliberately isolated legacy fixture
python3 scripts/bot-approver.py
```

**`scripts/reconciliation-loop.py`** — recovers a PR that never got a gate verdict at all (a lost
webhook), by pushing a real empty commit through the normal push path. It cannot make a red PR green,
only un-stick one that never got evaluated:

```sh
export GITEA_URL=http://127.0.0.1:3500
export GITEA_RECONCILE_TOKEN=<its own token> GITEA_RECONCILE_USER=<its own account>
export REPO_OWNER=<owner> REPO_NAME=<repo>
python3 scripts/reconciliation-loop.py
```

Run both as long-lived background processes (a terminal you leave open, `nohup`, a systemd unit, a
scheduled task — whatever your environment supports; there's no packaged service unit for either yet).

## 6. Ongoing operations

- **Trivy vulnerability DB freshness.** `pipelines/trivy-db-refresh.woodpecker.yml` exists and works,
  but **the Woodpecker cron job is not registered anywhere automatically** — register it yourself via
  Woodpecker's own cron UI/API against this platform's own repo. Until you do, the fast gate
  self-primes an empty cache on first use, which prevents outright failure but gives you no freshness
  guarantee.
- **Updating the scanner ruleset.** `policy/vendored-rules/` is vendored, not pulled live — a rule
  change is a reviewed PR to this repo like any other change, specifically so an upstream rule update
  can never silently block every onboarded repo's PRs with no review point.
- **Secrets on disk.** `.env` and any token files are plaintext today — `sops`/`age` encryption is
  named in `DESIGN.md` but not implemented. Treat every host this runs on accordingly, and never
  commit `.env`.
- **No monitoring stack yet.** No Prometheus/Grafana in `compose/minimal`. If a repo's gate looks
  wrong, your first move is `docker compose logs`, the Woodpecker build log for the pipeline in
  question, and `bot-approver.py`'s own stdout.

## 7. What genuinely isn't built yet — don't promise these

Read straight from `docs/ARCHITECTURE.md`'s current-state table and `docs/what_next.md` before telling
a real team any of the following exists:

- **The durable trusted-runner path** (`scripts/trusted-gate-runner.py` + `gate-bundle/`) — built,
  unit-tested, **never deployed**. The active protection is a transitional file/tree comparison
  (`GATE_CONTRACT_ENFORCE=1`), not an independently-executed, tamper-proof gate.
- **The two-party exception workflow** (`EXCEPTIONS.md`) — designed in full, **not built**. Baseline
  gating (section 4 above) is a different, already-built mechanism for *pre-existing* findings; it does
  not cover a genuinely new finding someone needs to formally accept.
- **Pre-receive secret detection** — the mechanism is live-tested (`compose/forgery-test/`) but not
  installed on any real onboarded repo's actual pre-receive hook.
- **Multi-repo standing services** for `bot-approver.py`/`reconciliation-loop.py` — both are
  single-repo, hand-started processes today.
- **Evidence storage, backups, restore drills, monitoring** — none of this exists in `minimal`. If this
  platform is ever load-bearing for a real team, this is the gap that matters most operationally.
- **A developer-facing portal** — everything with a screenshot in `DEVELOPER_HOWTO.md` is a design
  preview. Developers use Gitea's and Woodpecker's own native screens today.

## 8. Troubleshooting quick reference

| Symptom | Likely cause |
|---|---|
| Woodpecker sessions keep getting invalidated | `WOODPECKER_GRPC_SECRET` wasn't set explicitly in `.env` — it's regenerating on every restart |
| A pipeline step can't reach Gitea from a host-side script | Missing `Host: gitea:3500` header on the request — Gitea derives webhook routing from the request's `Host` header, not just `ROOT_URL` |
| `bot-approver.py` never votes on a PR | Check its own log output first — it prints exactly why it skipped (gate-contract mismatch, scanning steps not all green, or no pipeline run yet for that head) |
| Onboarding 403s on its own re-run | The automation account isn't in the branch protection push-whitelist — check step 5 of section 4 actually completed |
| Everything in Compose is missing/crash-looping | Docker Desktop instability — `docker compose up -d` again; this has been a recurring environmental issue on shared/loaded machines, not a code bug |
| A literal `${...}` breaks pipeline YAML parsing, even inside a comment | Woodpecker's server-side template substitution consumes dollar-brace patterns file-wide before real YAML parsing — never write that shape anywhere in a `.woodpecker.yml`, comments included, unless you mean Woodpecker to substitute it |

## 9. Where to go deeper

- [`GATE_CONTRACT.md`](GATE_CONTRACT.md) — exactly what the bot verifies before approving, and the
  transitional-vs-durable trust boundary in full.
- [`ARCHITECTURE.md`](ARCHITECTURE.md) — current-state table for every component, cross-referenced to
  the SADR that proved it.
- [`docs/adr/`](adr/) — every decision, each with live evidence, not just design intent.
- [`docs/TODO.md`](TODO.md) and [`docs/what_next.md`](what_next.md) — the actual current priority list.
  Re-check these before making any operational promise this document doesn't cover.
