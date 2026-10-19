#!/usr/bin/env bash
set -euo pipefail

# Generates local-dev-only JWT signing keys. NEVER reuse outside local dev!
KEYS_DIR="$(dirname "$0")/../deploy/keys"
mkdir -p "${KEYS_DIR}"

PRIVATE_KEY="${KEYS_DIR}/jwt_rs256.key"
PUBLIC_KEY="${KEYS_DIR}/jwt_rs256.pub"

if [ -f "${PRIVATE_KEY}" ] && [ -f "${PUBLIC_KEY}" ]; then
  echo "Keys already exist at ${KEYS_DIR}. Skipping generation."
  exit 0
fi

echo "Generating 2048-bit RSA private key for local dev..."
openssl genpkey -algorithm RSA -out "${PRIVATE_KEY}" -pkeyopt rsa_keygen_bits:2048

echo "Extracting RSA public key..."
openssl rsa -pubout -in "${PRIVATE_KEY}" -out "${PUBLIC_KEY}"

chmod 600 "${PRIVATE_KEY}"
chmod 644 "${PUBLIC_KEY}"

echo "Generated: ${PRIVATE_KEY} and ${PUBLIC_KEY}"

