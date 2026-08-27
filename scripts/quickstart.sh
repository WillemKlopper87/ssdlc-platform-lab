#!/bin/sh
# Bring up the minimal local profile after a safe, explicit preflight.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)
COMPOSE_DIR="${ROOT_DIR}/compose/minimal"
TERRAFORM_DIR="${ROOT_DIR}/terraform/local"

if [ ! -f "${COMPOSE_DIR}/.env" ]; then
  cp "${COMPOSE_DIR}/.env.example" "${COMPOSE_DIR}/.env"
  printf 'Created %s/.env from the template. Replace all placeholders, register the Gitea OAuth application, then rerun this command.\n' "$COMPOSE_DIR" >&2
  exit 1
fi

"${SCRIPT_DIR}/doctor.sh"

printf 'Provisioning Terraform-managed network and volumes...\n'
terraform -chdir="${TERRAFORM_DIR}" init
terraform -chdir="${TERRAFORM_DIR}" apply

printf 'Starting the minimal profile...\n'
docker compose --env-file "${COMPOSE_DIR}/.env" -f "${COMPOSE_DIR}/docker-compose.yml" up -d
printf 'Gitea: http://127.0.0.1:3500\nWoodpecker: http://127.0.0.1:%s\n' "$(grep '^WOODPECKER_PORT=' "${COMPOSE_DIR}/.env" | cut -d= -f2)"
