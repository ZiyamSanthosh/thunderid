#!/bin/sh

set -eu

actual_version="$(sed 's/^v//' version.txt | tr -d '\r\n')"
if [ "$actual_version" != "$SOURCE_PRODUCT_VERSION" ]; then
    echo "source image version mismatch: expected $SOURCE_PRODUCT_VERSION, found $actual_version" >&2
    exit 1
fi

for store in configdb entitydb runtime_transient runtime_persistent; do
    if [ ! -f "dbscripts/$store/postgres.sql" ]; then
        echo "source image is missing dbscripts/$store/postgres.sql" >&2
        exit 1
    fi
done

cp -R dbscripts/. /source-schemas/
