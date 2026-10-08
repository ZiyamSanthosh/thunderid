#!/bin/sh

# Copyright 2026 The ThunderID Authors
# SPDX-License-Identifier: Apache-2.0

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
migration_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)

compose_project=${MIGRATION_POC_PROJECT:-thunderid-migration-poc}
source_version=${SOURCE_PRODUCT_VERSION:-1.0.1}
temp_root=${TMPDIR:-/tmp}
temp_root=${temp_root%/}
poc_runtime_dir=${MIGRATION_POC_RUNTIME_DIR:-$temp_root/thunderid-migration-poc}

export COMPOSE_PROJECT_NAME=$compose_project
export SOURCE_PRODUCT_VERSION=$source_version
export SOURCE_THUNDERID_IMAGE=${SOURCE_THUNDERID_IMAGE:-ghcr.io/thunder-id/thunderid:$source_version}
export MIGRATION_DATABASE_LAYOUT=separate

echo "Removing containers, network, and database volumes for $compose_project..."
docker compose --project-directory "$migration_dir" down --volumes --remove-orphans

case "$poc_runtime_dir" in
	*/thunderid-migration-poc)
		rm -rf -- "$poc_runtime_dir"
		;;
	*)
		echo "refusing to remove unexpected runtime directory: $poc_runtime_dir" >&2
		exit 1
		;;
esac

echo "Migration POC database resources were removed."
