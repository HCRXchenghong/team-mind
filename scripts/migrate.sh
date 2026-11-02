#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Run against the Docker Compose MySQL service. An alternate database name
# lets CI verify an empty schema without touching the application database.
db_name="${1:-teammind}"
if [[ ! "$db_name" =~ ^[a-zA-Z][a-zA-Z0-9_]*$ ]]; then
  echo "Invalid database name" >&2
  exit 2
fi
mysql_in_container() {
	docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql --user=root "$1"' sh "$db_name"
}
docker compose exec -T mysql sh -c '
  db_name="$1"
  case "$MYSQL_USER" in
    ""|*[!a-zA-Z0-9_]*) echo "MYSQL_USER must contain only letters, digits, and underscores" >&2; exit 2 ;;
  esac
  MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --user=root -e "CREATE DATABASE IF NOT EXISTS $db_name CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"
' sh "$db_name"
existing_tables="$(mysql_in_container <<< 'SHOW TABLES;' | tail -n +2 | wc -l)"
if (( existing_tables > 0 )); then
  migration_table="$(mysql_in_container <<< "SHOW TABLES LIKE 'schema_migrations';" | tail -n +2 | wc -l)"
  if (( migration_table == 0 )); then
    echo "Database $db_name has tables but no migration history. Use an empty database or reconcile the schema manually before migrating." >&2
    exit 1
  fi
  applied_versions="$(mysql_in_container <<< 'SELECT COUNT(*) FROM schema_migrations;' | tail -n 1)"
  if (( applied_versions == 0 && existing_tables > 1 )); then
    echo "Database $db_name has application tables but no applied migration versions. Refusing to rerun DDL over existing data." >&2
    exit 1
  fi
fi
mysql_in_container <<'SQL'
CREATE TABLE IF NOT EXISTS schema_migrations (
  version VARCHAR(40) PRIMARY KEY,
  applied_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
SQL
for migration in backend/migrations/*.up.sql; do
  version="${migration##*/}"
  version="${version%.up.sql}"
  applied="$(mysql_in_container <<< "SELECT COUNT(*) FROM schema_migrations WHERE version='$version';" | tail -n 1)"
  if [[ "$applied" == 1 ]]; then
    echo "skip $version"
    continue
  fi
  mysql_in_container < "$migration"
  mysql_in_container <<< "INSERT INTO schema_migrations(version) VALUES('$version');"
  echo "applied $version"
done
docker compose exec -T mysql sh -c '
  db_name="$1"
  MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --user=root <<SQL
REVOKE IF EXISTS ALL PRIVILEGES ON $db_name.* FROM '\''$MYSQL_USER'\''@'\''%'\'';
GRANT SELECT, INSERT, UPDATE, DELETE ON $db_name.* TO '\''$MYSQL_USER'\''@'\''%'\'';
SQL
' sh "$db_name"
