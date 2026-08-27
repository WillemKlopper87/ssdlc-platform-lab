#!/bin/sh
# Vanilla postgres:16-alpine has no POSTGRES_MULTIPLE_DATABASES support
# (that's a third-party image feature) — this script does the same job
# on first boot only, via docker-entrypoint-initdb.d.
set -e

if [ -n "$POSTGRES_MULTIPLE_DATABASES" ]; then
  echo "creating multiple databases: $POSTGRES_MULTIPLE_DATABASES"
  OLDIFS=$IFS
  IFS=','
  for db in $POSTGRES_MULTIPLE_DATABASES; do
    IFS=$OLDIFS
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-EOSQL
      CREATE DATABASE "$db";
      GRANT ALL PRIVILEGES ON DATABASE "$db" TO "$POSTGRES_USER";
EOSQL
  done
  IFS=$OLDIFS
fi
