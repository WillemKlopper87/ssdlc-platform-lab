#!/bin/sh
# tests/regression/run-minimal-suite.sh
#
# Runs SADR-0011's bot-approver test, which needs the full compose/minimal
# stack (Postgres, Gitea, Woodpecker server+agent, real pipeline runs) --
# not the lightweight forgery-test harness the other four regression tests
# use (see run-forgery-suite.sh). Kept separate deliberately, to avoid
# paying Woodpecker's startup and OAuth-login cost when only Gitea-level
# checks are needed.
#
# Backs up and restores any existing compose/minimal/.env rather than
# overwriting a developer's own in-progress environment -- this suite
# needs full control of secrets/OAuth credentials to be reproducible, but
# that is this suite's business alone, not something that should survive
# past the run or clobber real work.
#
# Usage: ./run-minimal-suite.sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
COMPOSE_DIR="${SCRIPT_DIR}/../../compose/minimal"
ENV_BACKUP=""

cleanup() {
  echo ""
  echo "==> tearing down compose/minimal"
  (cd "$COMPOSE_DIR" && docker compose down -v) >/dev/null 2>&1 || true
  if [ -n "$ENV_BACKUP" ] && [ -f "$ENV_BACKUP" ]; then
    mv -f "$ENV_BACKUP" "${COMPOSE_DIR}/.env"
    echo "==> restored your original compose/minimal/.env"
  elif [ -z "$ENV_BACKUP" ]; then
    rm -f "${COMPOSE_DIR}/.env"
  fi
}
trap cleanup EXIT

if [ -f "${COMPOSE_DIR}/.env" ]; then
  ENV_BACKUP=$(mktemp)
  cp "${COMPOSE_DIR}/.env" "$ENV_BACKUP"
  echo "==> backed up your existing compose/minimal/.env"
fi

echo "==> generating fresh secrets for this run"
POSTGRES_PASSWORD=$(openssl rand -hex 16)
WOODPECKER_AGENT_SECRET=$(openssl rand -hex 32)
WOODPECKER_GRPC_SECRET=$(openssl rand -hex 32)
cat > "${COMPOSE_DIR}/.env" <<EOF
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
WOODPECKER_AGENT_SECRET=${WOODPECKER_AGENT_SECRET}
WOODPECKER_GRPC_SECRET=${WOODPECKER_GRPC_SECRET}
GITEA_OAUTH_CLIENT_ID=pending
GITEA_OAUTH_CLIENT_SECRET=pending
EOF

# shellcheck source=lib.sh
. "${SCRIPT_DIR}/lib.sh"

export GITEA="http://127.0.0.1:3500"
export GITEA_INTERNAL="http://gitea:3500"
export WOODPECKER="http://127.0.0.1:8000"
export FORGERY_CONTAINER="ssdlc-minimal-gitea"   # matches lib.sh's gitea_create_user contract

echo "==> bringing up postgres"
(cd "$COMPOSE_DIR" && docker compose up -d postgres) >/dev/null

echo "==> bringing up gitea"
(cd "$COMPOSE_DIR" && docker compose up -d gitea) >/dev/null
wait_for_gitea_healthy "$GITEA"

echo "==> bootstrapping admin account and OAuth app"
gitea_create_user "$GITEA" "$FORGERY_CONTAINER" gateadmin Gateadmin123! gateadmin@example.com --admin
GITEA_ADMIN_TOKEN=$(gitea_mint_token "$GITEA" gateadmin 'Gateadmin123!' setup-token '["write:admin","write:repository","write:user","read:issue"]')
export GITEA_ADMIN_TOKEN

oauth_json=$(curl -s -X POST "${GITEA}/api/v1/user/applications/oauth2" -H "Authorization: token ${GITEA_ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"woodpecker","redirect_uris":["http://127.0.0.1:8000/authorize"],"confidential_client":true}')
CLIENT_ID=$(echo "$oauth_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['client_id'])")
CLIENT_SECRET=$(echo "$oauth_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['client_secret'])")

python3 - "$CLIENT_ID" "$CLIENT_SECRET" "${COMPOSE_DIR}/.env" <<'PYEOF'
import sys
cid, csec, path = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path) as f:
    lines = f.readlines()
out = []
for line in lines:
    if line.startswith("GITEA_OAUTH_CLIENT_ID="):
        out.append(f"GITEA_OAUTH_CLIENT_ID={cid}\n")
    elif line.startswith("GITEA_OAUTH_CLIENT_SECRET="):
        out.append(f"GITEA_OAUTH_CLIENT_SECRET={csec}\n")
    else:
        out.append(line)
with open(path, "w") as f:
    f.writelines(out)
PYEOF

echo "==> bringing up woodpecker"
(cd "$COMPOSE_DIR" && docker compose up -d woodpecker-server woodpecker-agent) >/dev/null
wait_for_woodpecker_healthy "$WOODPECKER"

echo "==> minting a durable Woodpecker API token"
WOODPECKER_PAT=$(woodpecker_get_pat "$GITEA_INTERNAL" "$WOODPECKER" gateadmin 'Gateadmin123!' "$CLIENT_ID" "$GITEA")
export WOODPECKER_PAT
if [ -z "$WOODPECKER_PAT" ]; then
  echo "FATAL: could not mint a Woodpecker API token -- see docs/adr/0011 for the expected login sequence" >&2
  exit 1
fi

# shellcheck source=05-bot-approver.sh
. "${SCRIPT_DIR}/05-bot-approver.sh"
test_bot_approver

# shellcheck source=07-gate-contract-bypass.sh
# Reuses 05-bot-approver.sh's Woodpecker polling helpers (_wp_wait_for_pipeline
# etc.) -- must be sourced after it, not before.
. "${SCRIPT_DIR}/07-gate-contract-bypass.sh"
test_gate_contract_bypass

regression_summary
