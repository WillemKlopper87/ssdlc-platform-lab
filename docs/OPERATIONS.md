# Operations — minimal profile

Start the minimal local profile with `scripts/quickstart.sh`. It runs the
read-only `scripts/doctor.sh` preflight, then Terraform creates the external
network and volumes before Compose starts services. Do not substitute a plain
`docker compose up`: Compose must not silently recreate Terraform-owned state.

Copy `compose/minimal/.env.example` to `.env`, generate unique secrets, and
register the Gitea OAuth application before starting. The `.env` contains
secrets and must never be committed.

## Local endpoints

- Gitea: `http://127.0.0.1:3500`
- Woodpecker: `http://127.0.0.1:${WOODPECKER_PORT}` (default: `8000`)

If port 8000 is occupied, set `WOODPECKER_PORT` in `.env` to an unused port.
This changes Woodpecker's listener and published port together. Gitea delivers
webhooks inside the shared network namespace; changing only the host mapping
leaves the UI reachable but prevents pipeline runs.
Update the Gitea OAuth application's redirect URL to use the same port before
starting if you change it.

## Rules learned from live incidents

Three rules, paid for in real debugging time across several SADRs. Keep this section verbatim rather
than re-paraphrasing it — it has already drifted once and cost time to rediscover.

**Never write a literal `${...}`-shaped string anywhere in a `.woodpecker.yml`, including comments,
unless it's a value Woodpecker is meant to substitute.** Woodpecker's server-side template
substitution consumes dollar-brace patterns file-wide, before real YAML parsing — including inside
comments describing the syntax. (SADR-0007; hit again and re-confirmed in SADR-0016 when this
project's own pipeline header comment violated the rule it was describing.)

**Every host-side call that can trigger a Gitea webhook — pushes *and* PR creation, not pushes
alone — needs an explicit `Host: gitea:3500` header.** Gitea derives `clone_url` and webhook routing
from the triggering request's `Host` header, not solely from `ROOT_URL`. This has cost real time three
separate times (SADR-0008, re-confirmed and extended in SADR-0011 after forgetting it and hitting the
same bug again). `tests/regression/lib.sh` now shadows `curl` itself so no call site targeting Gitea's
port has to remember it; `git_push_origin` sets it unconditionally too, since git's own HTTP calls
don't go through the shadowed `curl`. That shadow is scoped to the test suite only — any *production*
script that talks to Gitea from outside its own Docker network (`onboard-repo.sh`, a future
bootstrap script) needs the same header applied deliberately, with an environment-appropriate
hostname, not the test suite's hard-coded value.

**To script Woodpecker's API, use `GET /web-config.js` with a session cookie for a legitimately-issued
CSRF token, then `POST /api/user/token` once for a durable PAT — never read the CSRF secret from the
database.** (SADR-0004 decision item 3, closed for real in SADR-0011.) The durable PAT this produces
is what every automation script (`onboard-repo.sh`, `bot-approver.py`) should hold — this login dance
is a one-time interactive step, not something to re-derive on every run.

## Trusted-runner pilot (do not enable prematurely)

The current per-repository Woodpecker gate is transitional. The next pilot
profile runs `scripts/trusted-gate-runner.py` in a separate, platform-owned
worker. It downloads the exact pull-request head with a **read-only** Gitea
token, runs an operator-owned gate bundle, and writes an authenticated result
for the bot. It does not read `.woodpecker.yml` or execute a command from the
application checkout.

Before enabling it, provision an isolated runner and set:

```text
GITEA_URL=https://gitea.example.internal
GITEA_RUNNER_TOKEN=<read-only repository token>
REPO_OWNER=<pilot owner>
REPO_NAME=<pilot repository>
GATE_BUNDLE_COMMAND=/opt/ssdlc-gate/run-container.sh
GATE_BUNDLE_IMAGE=registry.example.internal/ssdlc-gate@sha256:<immutable-image-digest>
GATE_ATTESTATIONS_DIR=/var/lib/ssdlc/attestations
GATE_ATTESTATION_KEY=<shared runner/bot secret>
```

The bundle receives the checked-out source path through `GATE_WORKSPACE` and
must write `result.json` to `GATE_OUTPUT_DIR`, for example:

```json
{"decision":"pass","scanners":{"secrets":"success","sast":"success","dependencies":"success"}}
```

Mount the attestation directory read-only into the bot sidecar. Use a different
account/token for the bot; do not give the runner any forge write permission or
the bot approval token. After a normal and a deliberately altered PR have been
tested, set `GATE_ATTESTATION_REQUIRED=1` on the bot and set both
`GATE_CONTRACT_DIGEST` (the output of `scripts/print-gate-contract-digest.py`)
and `GATE_POLICY_DIGEST` (the output of `scripts/print-policy-digest.py`,
run against the exact `policy/` tree baked into the bundle image currently
deployed). The bot then ignores Woodpecker pipeline success and requires the
matching runner result instead — including that the bundle's own scanning
policy matches what was reviewed, not just that some scan ran. Re-run
`print-policy-digest.py` and update `GATE_POLICY_DIGEST` on the bot every time
the bundle image is rebuilt with a rule change; a stale value means every
attestation from the new bundle fails closed rather than silently trusting
an unreviewed rule change.
