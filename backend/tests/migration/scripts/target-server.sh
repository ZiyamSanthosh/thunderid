#!/bin/sh
# Starts the server compiled from this checkout.
set -eu

cd /opt/thunderid
sh /test/generate-material.sh
exec ./thunderid -serverHome /opt/thunderid
