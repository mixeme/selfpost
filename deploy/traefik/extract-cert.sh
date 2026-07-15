#!/bin/sh
# Extracts a PEM cert/key pair for one domain out of Traefik's acme.json
# (spec 10.3: "Traefik — сертификаты в acme.json, потребуется шаг извлечения
# PEM"). Run this on the host, after Traefik has issued or renewed the
# certificate, and again on a schedule (cron/systemd timer) since acme.json
# is not itself watched by SelfPost/Postfix.
#
# Requires jq. Usage: ./extract-cert.sh <acme.json path> <domain> <output dir>
set -eu

ACME_JSON="${1:?path to acme.json}"
DOMAIN="${2:?domain name, e.g. mail.example.com}"
OUT_DIR="${3:?output directory, e.g. ./extracted-certs}"

mkdir -p "$OUT_DIR"

jq -r --arg domain "$DOMAIN" '
  .le.Certificates[]
  | select(.domain.main == $domain)
  | .certificate' "$ACME_JSON" | base64 -d > "$OUT_DIR/fullchain.pem"

jq -r --arg domain "$DOMAIN" '
  .le.Certificates[]
  | select(.domain.main == $domain)
  | .key' "$ACME_JSON" | base64 -d > "$OUT_DIR/privkey.pem"

chmod 0640 "$OUT_DIR/fullchain.pem" "$OUT_DIR/privkey.pem"
echo "extracted $DOMAIN to $OUT_DIR/{fullchain,privkey}.pem"
