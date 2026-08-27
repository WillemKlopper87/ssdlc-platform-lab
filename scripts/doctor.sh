#!/bin/sh
# Read-only preflight for the minimal local profile.  It intentionally makes
# no Docker or Terraform changes, so it is safe to run before quickstart.sh.
set -u

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)
COMPOSE_DIR="${ROOT_DIR}/compose/minimal"
FAILED=0

check_command() {
  if command -v "$1" >/dev/null 2>&1; then
    printf 'PASS: %s available\n' "$1"
  else
    printf 'FAIL: %s is required\n' "$1" >&2
    FAILED=1
  fi
}

check_command docker
check_command terraform

if command -v docker >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1; then
    printf 'PASS: docker compose available\n'
  else
    printf 'FAIL: Docker Compose v2 is required\n' >&2
    FAILED=1
  fi
  if docker info >/dev/null 2>&1; then
    printf 'PASS: Docker daemon reachable\n'
  else
    printf 'FAIL: Docker daemon is not reachable\n' >&2
    FAILED=1
  fi
fi

if [ ! -f "${COMPOSE_DIR}/.env" ]; then
  printf 'FAIL: %s/.env is missing; copy .env.example and replace every placeholder\n' "$COMPOSE_DIR" >&2
  FAILED=1
elif grep -Eq '^(POSTGRES_PASSWORD=change-me|WOODPECKER_AGENT_SECRET=generate-|WOODPECKER_GRPC_SECRET=generate-|GITEA_OAUTH_CLIENT_ID=from-|GITEA_OAUTH_CLIENT_SECRET=from-)' "${COMPOSE_DIR}/.env"; then
  printf 'FAIL: %s/.env still contains a placeholder secret or OAuth value\n' "$COMPOSE_DIR" >&2
  FAILED=1
else
  printf 'PASS: .env exists without template placeholders\n'
fi

if [ "$FAILED" -eq 0 ] && docker compose --env-file "${COMPOSE_DIR}/.env" -f "${COMPOSE_DIR}/docker-compose.yml" config --quiet; then
  printf 'PASS: Compose configuration is valid\n'
else
  [ "$FAILED" -ne 0 ] || printf 'FAIL: Compose configuration is invalid\n' >&2
  FAILED=1
fi

if [ "$FAILED" -eq 0 ]; then
  printf 'Doctor: ready for scripts/quickstart.sh\n'
else
  printf 'Doctor: fix the failures above before starting the profile\n' >&2
fi
exit "$FAILED"
