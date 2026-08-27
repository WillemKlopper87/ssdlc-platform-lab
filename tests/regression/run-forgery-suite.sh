#!/bin/sh
# tests/regression/run-forgery-suite.sh
#
# Runs the five regression tests that only need Gitea (no Woodpecker) --
# SADR-0001, 0002, 0003, 0009, 0013 -- against a single compose/forgery-test
# harness brought up and torn down once for all five, per this project's
# own "nothing here is meant to persist" discipline for this harness.
#
# SADR-0011's bot-approver test needs the full compose/minimal stack
# (Woodpecker, real pipeline runs) and lives in run-minimal-suite.sh
# instead -- deliberately not combined here, to keep this suite fast and
# avoid paying Woodpecker's startup cost when only the Gitea-level checks
# are needed.
#
# Usage: ./run-forgery-suite.sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
HARNESS_DIR="${SCRIPT_DIR}/../../compose/forgery-test"

cleanup() {
  echo ""
  echo "==> tearing down forgery-test harness"
  (cd "$HARNESS_DIR" && docker compose down -v) >/dev/null 2>&1 || true
  # docker compose down -v only removes NAMED volumes -- forgery-test's
  # docker-compose.yml bind-mounts ./data, which -v never touches. Found
  # live: state from an earlier run collided with this suite's fixed
  # account names ("user already exists: gateadmin") on the very first
  # attempt to make this suite idempotent. Force it every time.
  rm -rf "${HARNESS_DIR}/data" 2>/dev/null || true
}
trap cleanup EXIT

echo "==> clean slate: removing any leftover forgery-test state"
cleanup

echo "==> bringing up forgery-test harness"
(cd "$HARNESS_DIR" && docker compose up -d) >/dev/null

# shellcheck source=lib.sh
. "${SCRIPT_DIR}/lib.sh"

export GITEA="http://localhost:3500"
export FORGERY_CONTAINER="forgery-test-gitea"

echo "==> waiting for gitea"
wait_for_gitea_healthy "$GITEA"

echo "==> bootstrapping shared admin account"
gitea_create_user "$GITEA" "$FORGERY_CONTAINER" gateadmin Gateadmin123! gateadmin@example.com --admin
export GITEA_ADMIN_TOKEN
GITEA_ADMIN_TOKEN=$(gitea_mint_token "$GITEA" gateadmin 'Gateadmin123!' setup-token '["write:admin","write:repository","write:user","read:issue","write:organization"]')

# shellcheck source=01-status-forgery.sh
. "${SCRIPT_DIR}/01-status-forgery.sh"
test_status_forgery

# shellcheck source=02-pre-receive-secrets.sh
. "${SCRIPT_DIR}/02-pre-receive-secrets.sh"
test_pre_receive_secrets

# shellcheck source=03-pr-retargeting.sh
. "${SCRIPT_DIR}/03-pr-retargeting.sh"
test_pr_retargeting

# shellcheck source=04-approval-check.sh
. "${SCRIPT_DIR}/04-approval-check.sh"
test_approval_check

# shellcheck source=06-team-whitelist.sh
. "${SCRIPT_DIR}/06-team-whitelist.sh"
test_team_whitelist

regression_summary
