#!/bin/sh
# Generates the keys, certificates and secrets a server needs to start, once, into the shared /material volume,
# and links them into the server home. The previous server and the target server share the volume, because data
# the previous one encrypted has to be readable by the target one.
set -eu

home=/opt/thunderid
mkdir -p /material/certs /material/secrets

certificate() {
    [ -f "/material/certs/$1.cert" ] && return 0
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout "/material/certs/$1.key" \
        -out "/material/certs/$1.cert" -subj "/CN=localhost" -addext "subjectAltName=DNS:localhost" >/dev/null 2>&1
}
certificate server
certificate signing
if [ ! -f /material/certs/ecdsa-signing.cert ]; then
    openssl ecparam -name prime256v1 -genkey -noout -param_enc named_curve -out /material/certs/ecdsa-signing.key
    openssl req -new -x509 -nodes -days 365 -key /material/certs/ecdsa-signing.key \
        -out /material/certs/ecdsa-signing.cert -subj "/CN=localhost" \
        -addext "subjectAltName=DNS:localhost" >/dev/null 2>&1
fi
[ -f /material/certs/crypto.key ] || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' > /material/certs/crypto.key
[ -f /material/secrets/direct_auth_secret ] || printf '%s' secret > /material/secrets/direct_auth_secret

rm -rf "$home/config/certs" "$home/config/secrets"
ln -s /material/certs "$home/config/certs"
ln -s /material/secrets "$home/config/secrets"
