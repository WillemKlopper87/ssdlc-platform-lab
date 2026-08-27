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

Never write a literal dollar-brace template pattern anywhere in a
`.woodpecker.yml`, including comments, unless Woodpecker should substitute it:
Woodpecker expands those patterns before YAML parsing.

Host-side calls that must trigger Gitea webhooks need a Host header Gitea can
route internally. The regression helper handles its local-test header;
production automation needs an environment-appropriate hostname, not that
hard-coded test value.

For Woodpecker automation, use a durable personal access token from the normal
login flow. Do not read or reconstruct CSRF secrets from the database.

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
tested, set `GATE_ATTESTATION_REQUIRED=1` on the bot and set
`GATE_CONTRACT_DIGEST` to the output of
`scripts/print-gate-contract-digest.py`. The bot then ignores Woodpecker
pipeline success and requires the matching runner result instead.
