#!/bin/bash
# Extracts a PEM cert/key pair for one domain out of Traefik's acme.json —
# Traefik keeps its certificates there, so a PEM extraction step is required.
# Run this on the host, after Traefik has issued or renewed the certificate, and
# again on a schedule (cron/systemd timer) since acme.json is not itself watched
# by SelfPost/Postfix.
#
# Requires bash and jq. Usage: ./extract-cert.sh <acme.json path> <domain> <output dir>
#
# The layout is not documented (doc.traefik.io/traefik/v3.1/https/acme/ says
# only "JSON file"); read in Traefik v3.1.0: pkg/provider/acme/local_store.go
# keeps map[resolver name]*StoredData, store.go StoredData{Account,
# Certificates}, provider.go Certificate{domain, certificate, key} with the PEM
# as []byte - base64 in JSON (Go encoding/json). "le" is the resolver
# docker-compose.traefik.yml names.
#
# Nothing is replaced unless both new files are PEM: a domain acme.json does
# not hold, an empty field or a broken file stops the script with an error and
# leaves the files that were there (audit 2026-10-12, M21 - under sh without
# pipefail, jq finding nothing still exited 0 and two empty files replaced the
# certificate; Postfix's TLS broke at its next restart). The new files are
# written beside the old ones, the owner's alone until they are whole, then
# renamed into place.
set -euo pipefail

ACME_JSON="${1:?path to acme.json}"
DOMAIN="${2:?domain name, e.g. mail.example.com}"
OUT_DIR="${3:?output directory, e.g. ./extracted-certs}"

mkdir -p "$OUT_DIR"
umask 077

die() {
	echo "extract-cert: $*" >&2
	exit 1
}

# field prints the base64 of one field of the domain's first certificate, or
# nothing.
field() {
	jq -r --arg domain "$DOMAIN" --arg field "$1" '
	  first(.le.Certificates[]? | select(.domain.main == $domain) | .[$field]) // empty
	' "$ACME_JSON"
}

cert_b64=$(field certificate)
key_b64=$(field key)
[ -n "$cert_b64" ] || die "$ACME_JSON holds no certificate for $DOMAIN"
[ -n "$key_b64" ] || die "$ACME_JSON holds no key for $DOMAIN"

tmp_cert="" tmp_key=""
trap 'rm -f "$tmp_cert" "$tmp_key"' EXIT
tmp_cert=$(mktemp "$OUT_DIR/.fullchain.pem.XXXXXX")
tmp_key=$(mktemp "$OUT_DIR/.privkey.pem.XXXXXX")

printf '%s' "$cert_b64" | base64 -d >"$tmp_cert" || die "the certificate of $DOMAIN is not base64"
printf '%s' "$key_b64" | base64 -d >"$tmp_key" || die "the key of $DOMAIN is not base64"
grep -q -- '-----BEGIN CERTIFICATE-----' "$tmp_cert" || die "the certificate of $DOMAIN is not PEM"
grep -q -- '-----BEGIN .*PRIVATE KEY-----' "$tmp_key" || die "the key of $DOMAIN is not PEM"

chmod 0640 "$tmp_cert" "$tmp_key"
# The group the files had stays theirs: the guide gives the key to group 10998
# so the panel can read it for a full backup, and a run from root's cron would
# otherwise hand it to root's group (review of audit stage 5).
for f in fullchain privkey; do
	if [ -e "$OUT_DIR/$f.pem" ]; then
		tmp=$tmp_cert
		[ "$f" = privkey ] && tmp=$tmp_key
		chgrp --reference="$OUT_DIR/$f.pem" "$tmp" || die "cannot give the new $f.pem the group of the old one"
	fi
done
mv -f "$tmp_cert" "$OUT_DIR/fullchain.pem"
mv -f "$tmp_key" "$OUT_DIR/privkey.pem"
trap - EXIT
echo "extracted $DOMAIN to $OUT_DIR/{fullchain,privkey}.pem"
