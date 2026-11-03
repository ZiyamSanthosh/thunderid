#!/bin/sh

set -eu

if [ "$MIGRATION_DATABASE_LAYOUT" = "shared" ]; then
    createdb --username "$POSTGRES_USER" thunderid
    for store in configdb entitydb runtime_transient runtime_persistent; do
        psql --set ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname thunderid \
            --file "/source-schemas/$store/postgres.sql"
    done
else
    for store in configdb entitydb runtime_transient runtime_persistent; do
        createdb --username "$POSTGRES_USER" "$store"
        psql --set ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$store" \
            --file "/source-schemas/$store/postgres.sql"
    done
fi

touch /var/lib/postgresql/data/.source-schema-initialized
