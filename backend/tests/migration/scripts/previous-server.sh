#!/bin/sh
# Runs the previous release's server image: "bootstrap" creates its default resources, "serve" starts the server.
set -eu

cd /opt/thunderid
sh /test/generate-material.sh
case "${1:-serve}" in
bootstrap)
    ADMIN_USERNAME=admin ADMIN_PASSWORD='Adm1n-Pass-123!' exec ./thunderid -serverHome /opt/thunderid bootstrap
    ;;
serve)
    exec ./thunderid -serverHome /opt/thunderid
    ;;
esac
