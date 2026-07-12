#!/bin/sh
set -eu

until pg_isready -h postgres -U "$POSTGRES_USER" -d "$POSTGRES_DB"; do
  sleep 1
done

for migration in /migrations/*.sql; do
  echo "Applying $migration"
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$migration"
done
