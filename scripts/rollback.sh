#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Require an explicit database name: down migrations may delete business data.
db_name="${1:-}"
if [[ -z "$db_name" || ! "$db_name" =~ ^[a-zA-Z][a-zA-Z0-9_]*$ ]]; then
  echo "Usage: scripts/rollback.sh <explicit-database-name>" >&2
  exit 2
fi

mysql_in_container() {
	docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql --user=root "$1"' sh "$db_name"
}

migration_table="$(mysql_in_container <<< "SHOW TABLES LIKE 'schema_migrations';" | tail -n +2 | wc -l)"
if (( migration_table == 0 )); then
  echo "Database $db_name has no migration history; refusing rollback." >&2
  exit 1
fi
version="$(mysql_in_container <<< 'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;' | tail -n 1)"
if [[ -z "$version" ]]; then
  echo "Database $db_name has no applied migration to roll back." >&2
  exit 1
fi
if [[ ! "$version" =~ ^[0-9]{6}_[a-z0-9_]+$ ]]; then
  echo "Unexpected migration version in database; refusing rollback." >&2
  exit 1
fi
migration="backend/migrations/${version}.down.sql"
if [[ ! -f "$migration" ]]; then
  echo "Rollback script is missing for $version." >&2
  exit 1
fi

# MySQL DDL is not transactional. Take a database backup before rollback and
# inspect the schema manually if any DDL statement fails part way through.
mysql_in_container < "$migration"
mysql_in_container <<< "DELETE FROM schema_migrations WHERE version='$version';"
echo "rolled back $version from $db_name"
