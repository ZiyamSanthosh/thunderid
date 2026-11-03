#!/bin/sh

# Copyright 2026 The ThunderID Authors
# SPDX-License-Identifier: Apache-2.0

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
migration_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
backend_dir=$(CDPATH= cd -- "$migration_dir/../.." && pwd)
repository_dir=$(CDPATH= cd -- "$backend_dir/.." && pwd)

compose_project=${MIGRATION_POC_PROJECT:-thunderid-migration-poc}
source_version=${SOURCE_PRODUCT_VERSION:-1.0.1}
source_image=${SOURCE_THUNDERID_IMAGE:-ghcr.io/thunder-id/thunderid:$source_version}
temp_root=${TMPDIR:-/tmp}
temp_root=${temp_root%/}
poc_runtime_dir=${MIGRATION_POC_RUNTIME_DIR:-$temp_root/thunderid-migration-poc}
product_version=$(sed 's/^v//' "$repository_dir/version.txt" | tr -d '\r\n')
distribution_zip=${THUNDERID_DISTRIBUTION_ZIP:-}

export COMPOSE_PROJECT_NAME=$compose_project
export SOURCE_PRODUCT_VERSION=$source_version
export SOURCE_THUNDERID_IMAGE=$source_image
export MIGRATION_DATABASE_LAYOUT=separate

if ! command -v docker >/dev/null 2>&1; then
	echo "docker is required to create the migration POC databases" >&2
	exit 1
fi
if ! command -v unzip >/dev/null 2>&1; then
	echo "unzip is required to extract the ThunderID distribution" >&2
	exit 1
fi
if ! docker info >/dev/null 2>&1; then
	echo "the Docker daemon is not available; start Docker Desktop or Rancher Desktop" >&2
	exit 1
fi
if [ -z "$distribution_zip" ]; then
	distribution_count=0
	for candidate in "$repository_dir"/target/dist/thunderid-"$product_version"-*.zip; do
		[ -f "$candidate" ] || continue
		distribution_zip=$candidate
		distribution_count=$((distribution_count + 1))
	done
	if [ "$distribution_count" -eq 0 ]; then
		echo "ThunderID $product_version distribution ZIP was not found under $repository_dir/target/dist" >&2
		echo "Run 'make build_backend' or 'make build' from $repository_dir first." >&2
		exit 1
	fi
	if [ "$distribution_count" -gt 1 ]; then
		echo "multiple ThunderID $product_version distribution ZIPs were found" >&2
		echo "set THUNDERID_DISTRIBUTION_ZIP to the ZIP for the current operating system" >&2
		exit 1
	fi
fi
if [ ! -f "$distribution_zip" ]; then
	echo "ThunderID distribution ZIP not found at $distribution_zip" >&2
	exit 1
fi

echo "Resetting the isolated $compose_project environment..."
docker compose --project-directory "$migration_dir" down --volumes --remove-orphans >/dev/null 2>&1 || true

echo "Creating PostgreSQL databases from ThunderID $source_version schemas..."
docker compose --project-directory "$migration_dir" up --detach --wait postgres

postgres_address=$(docker compose --project-directory "$migration_dir" port postgres 5432)
postgres_port=${postgres_address##*:}
if [ -z "$postgres_port" ]; then
	echo "failed to resolve the published PostgreSQL port" >&2
	exit 1
fi

case "$poc_runtime_dir" in
	*/thunderid-migration-poc) ;;
	*)
		echo "refusing to replace unexpected runtime directory: $poc_runtime_dir" >&2
		exit 1
		;;
esac
rm -rf -- "$poc_runtime_dir"
mkdir -p "$poc_runtime_dir"
unzip -q "$distribution_zip" -d "$poc_runtime_dir"

distribution_home=""
distribution_count=0
for candidate in "$poc_runtime_dir"/thunderid-"$product_version"-*; do
	[ -d "$candidate" ] || continue
	distribution_home=$candidate
	distribution_count=$((distribution_count + 1))
done
if [ "$distribution_count" -ne 1 ]; then
	echo "expected one ThunderID distribution directory in $distribution_zip, found $distribution_count" >&2
	exit 1
fi
thunderid_binary=$distribution_home/thunderid
if [ ! -x "$thunderid_binary" ]; then
	echo "ThunderID executable not found at $thunderid_binary" >&2
	exit 1
fi

# Distribution packages intentionally omit generated secrets and keys. Migration commands load the
# standard configuration, so provide the required configuration-encryption key without running the
# product bootstrap performed by setup.sh.
mkdir -p "$distribution_home/config/certs"
if command -v openssl >/dev/null 2>&1; then
	crypto_key=$(openssl rand -hex 32)
else
	crypto_key=$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')
fi
printf '%s' "$crypto_key" >"$distribution_home/config/certs/crypto.key"

cat >"$distribution_home/deployment.yaml" <<EOF
server:
  hostname: "localhost"
  port: 8090
  http_only: true

database:
  config:
    type: postgres
    postgres:
      hostname: 127.0.0.1
      port: $postgres_port
      name: configdb
      username: migration_user
      password: migration_password
      sslmode: disable

  runtime_transient:
    type: postgres
    postgres:
      hostname: 127.0.0.1
      port: $postgres_port
      name: runtime_transient
      username: migration_user
      password: migration_password
      sslmode: disable

  entity:
    type: postgres
    postgres:
      hostname: 127.0.0.1
      port: $postgres_port
      name: entitydb
      username: migration_user
      password: migration_password
      sslmode: disable

  runtime_persistent:
    type: postgres
    postgres:
      hostname: 127.0.0.1
      port: $postgres_port
      name: runtime_persistent
      username: migration_user
      password: migration_password
      sslmode: disable

log:
  level: info
EOF

echo "Initializing migration metadata at source level $source_version..."
"$thunderid_binary" -serverHome "$distribution_home" migration initialize

echo "Verifying the four-table migration metadata schema..."
for database in configdb entitydb runtime_transient runtime_persistent; do
	docker compose --project-directory "$migration_dir" exec -T postgres \
		psql --set ON_ERROR_STOP=1 --username migration_user --dbname "$database" <<'SQL'
DO $$
DECLARE
    metadata_table_count INTEGER;
    legacy_table_count INTEGER;
    state_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO metadata_table_count
    FROM INFORMATION_SCHEMA.TABLES
    WHERE TABLE_SCHEMA = CURRENT_SCHEMA()
      AND TABLE_NAME IN (
          'MIGRATION_STATE',
          'MIGRATION_PROGRESS',
          'MIGRATION_DEPLOYMENT_STATE',
          'MIGRATION_RUN'
      );

    SELECT COUNT(*) INTO legacy_table_count
    FROM INFORMATION_SCHEMA.TABLES
    WHERE TABLE_SCHEMA = CURRENT_SCHEMA()
      AND TABLE_NAME IN ('MIGRATION_RUN_TARGET', 'MIGRATION_FAILURE');

    SELECT COUNT(*) INTO state_count FROM "MIGRATION_STATE";

    IF metadata_table_count <> 4 THEN
        RAISE EXCEPTION 'expected four migration metadata tables, found %', metadata_table_count;
    END IF;
    IF legacy_table_count <> 0 THEN
        RAISE EXCEPTION 'found % legacy migration metadata tables', legacy_table_count;
    END IF;
    IF state_count <> 1 THEN
        RAISE EXCEPTION 'expected one initialized migration state, found %', state_count;
    END IF;
END
$$;
SQL
done
echo "Verified migration metadata in configdb, entitydb, runtime_transient, and runtime_persistent."

echo "Populating representative ThunderID $source_version data..."
docker compose --project-directory "$migration_dir" exec -T postgres \
	psql --set ON_ERROR_STOP=1 --username migration_user --dbname configdb <<'SQL'
INSERT INTO "ENTITY_TYPES"
    (DEPLOYMENT_ID, ID, CATEGORY, NAME, OU_ID, ALLOW_SELF_REGISTRATION, SCHEMA_DEF)
VALUES
    ('poc-deployment-a', '0199a123-4701-7abc-8def-0123456789ab', 'user', 'Legacy Person',
        '0199a123-4702-7abc-8def-0123456789ab', FALSE, '{}'::jsonb),
    ('poc-deployment-a', '0199a123-4704-7abc-8def-0123456789ab', 'user', 'legacy-person',
        '0199a123-4702-7abc-8def-0123456789ab', FALSE, '{}'::jsonb),
    ('poc-deployment-b', '0199a123-5701-7abc-8def-0123456789ab', 'user', 'Legacy Person',
        '0199a123-5702-7abc-8def-0123456789ab', FALSE, '{}'::jsonb),
    ('poc-deployment-b', '0199a123-5704-7abc-8def-0123456789ab', 'user', 'Secondary Entity',
        '0199a123-5702-7abc-8def-0123456789ab', FALSE, '{}'::jsonb);

INSERT INTO "ENTITY_TYPES"
    (DEPLOYMENT_ID, ID, CATEGORY, NAME, OU_ID, ALLOW_SELF_REGISTRATION, SCHEMA_DEF)
SELECT
    'poc-deployment-a',
    '0199a123-' || lpad((4800 + sequence_number)::text, 4, '0') || '-7abc-8def-0123456789ab',
    'user',
    'Batch Entity ' || lpad((sequence_number + 1)::text, 3, '0'),
    '0199a123-4702-7abc-8def-0123456789ab',
    FALSE,
    '{}'::jsonb
FROM generate_series(0, 98) AS sequence_number;
SQL

docker compose --project-directory "$migration_dir" exec -T postgres \
	psql --set ON_ERROR_STOP=1 --username migration_user --dbname entitydb <<'SQL'
INSERT INTO "ENTITY"
    (DEPLOYMENT_ID, ID, CATEGORY, TYPE, STATE, OU_ID, CREATED_AT, UPDATED_AT)
VALUES
    ('poc-deployment-a', '0199a123-4703-7abc-8def-0123456789ab', 'user', 'Legacy Person', 'ACTIVE',
        '0199a123-4702-7abc-8def-0123456789ab', NOW(), NOW());
SQL

cat >"$poc_runtime_dir/connection.env" <<EOF
THUNDERID_DISTRIBUTION_ZIP=$distribution_zip
THUNDERID_BINARY=$thunderid_binary
THUNDERID_SERVER_HOME=$distribution_home
POSTGRES_HOST=127.0.0.1
POSTGRES_PORT=$postgres_port
POSTGRES_USER=migration_user
POSTGRES_PASSWORD=migration_password
EOF

echo
echo "Migration POC environment is ready."
echo
echo "Distribution ZIP:  $distribution_zip"
echo "ThunderID binary: $thunderid_binary"
echo "Server home:      $distribution_home"
echo
echo "Run migration commands with:"
echo "  cd $distribution_home"
echo "  ./thunderid migration status"
echo "    (Expected to report incompatibility and exit non-zero before migration.)"
echo "  ./thunderid migration check expand"
echo "  ./thunderid migration apply expand"
echo "  ./thunderid migration check migrate"
echo "  ./thunderid migration apply migrate --deployment-id poc-deployment-a"
echo
echo "After the selective migration, inspect configdb with:"
echo "  SELECT DEPLOYMENT_ID, COUNT(*) AS TOTAL, COUNT(HANDLE) AS MIGRATED"
echo "  FROM \"ENTITY_TYPES\" GROUP BY DEPLOYMENT_ID ORDER BY DEPLOYMENT_ID;"
echo "  SELECT DEPLOYMENT_ID, COMPLETED_LEVEL FROM \"MIGRATION_DEPLOYMENT_STATE\";"
echo "  Expected: poc-deployment-a is migrated and poc-deployment-b is not."
echo
echo "Then complete and verify the store-wide migration with:"
echo "  ./thunderid migration apply migrate"
echo "  ./thunderid migration status"
echo "  ./thunderid migration check contract"
echo "  ./thunderid migration apply contract"
echo
echo "DBeaver PostgreSQL connection:"
echo "  Host:     127.0.0.1"
echo "  Port:     $postgres_port"
echo "  Username: migration_user"
echo "  Password: migration_password"
echo "  Databases: configdb, entitydb, runtime_transient, runtime_persistent"
echo
echo "Connection details were also written to $poc_runtime_dir/connection.env"
