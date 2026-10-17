#!/bin/sh
# Picks up a renewed TLS certificate while the container runs (spec 5.2 p.4;
# audit 2026-10-12, H2).
#
# Postfix does not read the mounted files: postfix-config.sh copies them at
# start into /etc/postfix/tls-internal, because Postfix wants the key owned by
# root with mode 0600, which a read-only bind mount owned by the host's user
# cannot give. A renewal of the mounted files therefore reaches Postfix only
# through a new copy. Every interval this compares the mounted files with the
# copy; when they differ it copies both beside their targets, checks that the
# copies - not the mounted files, which a renewer may be rewriting - are a
# certificate and the key that belongs to it, renames both over the targets
# and reloads Postfix. When nothing changed it does nothing.
# (Postfix 3.10.13: smtpd loads the certificate in pre_jail_init(), once per
# process, src/smtpd/smtpd.c:6442; "postfix reload" is SIGHUP to master,
# conf/postfix-script:277, after which the old smtpd processes exit on the closed
# status pipe, src/master/master_conf.c, master_config() ->
# master_restart_service(), and new ones load the files again.)
#
# Runs under supervisord as root. It sleeps first: at container start the
# wrapper is still bringing Postfix up. It never exits non-zero, so a failure
# does not trip the crashexit listener; it is logged and tried again.
set -u
umask 077 # the key's copy is never readable by others, not even for an instant

INTERVAL="${TLS_RELOAD_INTERVAL_SECONDS:-86400}"
SRC_CERT="${TLS_CERT_FILE:-/etc/postfix/tls/fullchain.pem}"
SRC_KEY="${TLS_KEY_FILE:-/etc/postfix/tls/privkey.pem}"
DIR=/etc/postfix/tls-internal
CERT="$DIR/fullchain.pem"
KEY="$DIR/privkey.pem"
PENDING=0 # a copy was renewed and Postfix has not been reloaded since

reload() {
	if postfix reload; then
		echo "cert-reload: postfix reloaded ($1)"
		PENDING=0
	else
		echo "cert-reload: postfix reload failed, will try again after ${INTERVAL}s" >&2
	fi
}

# same_pair says whether a certificate file and a private key file belong
# together, by comparing the public key each one prints. openssl-x509(1) and
# openssl-pkey(1) do not say the two print it byte for byte alike; measured
# in the image, 2026-10-15, OpenSSL 3.0.x of bookworm, before trixie's 3.5
# (re-read on 3.5.7 is due with the next look at this script): identical for RSA-2048, P-256 and
# P-384 keys, and for a P-256 key written in SEC1 form ("EC PRIVATE KEY") -
# certbot 2.0 and later issue ECDSA keys by default (assumptions C-X1).
same_pair() {
	cert_pub=$(openssl x509 -noout -pubkey -in "$1" 2>/dev/null) || return 1
	key_pub=$(openssl pkey -pubout -in "$2" 2>/dev/null) || return 1
	[ -n "$cert_pub" ] && [ "$cert_pub" = "$key_pub" ]
}

refresh() {
	if [ "$PENDING" = 1 ]; then
		reload "renewed certificate, earlier reload had failed"
	fi
	if [ "$(postconf -h smtpd_tls_cert_file 2>/dev/null)" != "$CERT" ]; then
		# Postfix reads the mounted files themselves (they were missing at
		# start, so no copy was made): a reload is what picks up a change.
		reload "periodic TLS refresh"
		return
	fi
	if [ ! -r "$SRC_CERT" ] || [ ! -r "$SRC_KEY" ]; then
		echo "cert-reload: cannot read ${SRC_CERT} or ${SRC_KEY}; Postfix keeps the certificate it has" >&2
		return
	fi
	if cmp -s "$SRC_CERT" "$CERT" && cmp -s "$SRC_KEY" "$KEY"; then
		return
	fi
	rm -f "$CERT.new" "$KEY.new"
	if ! cp "$SRC_CERT" "$CERT.new" || ! cp "$SRC_KEY" "$KEY.new"; then
		rm -f "$CERT.new" "$KEY.new"
		echo "cert-reload: copying the renewed certificate failed, will try again after ${INTERVAL}s" >&2
		return
	fi
	if ! same_pair "$CERT.new" "$KEY.new"; then
		rm -f "$CERT.new" "$KEY.new"
		echo "cert-reload: the mounted certificate and key differ from Postfix's copy but do not belong together; will look again after ${INTERVAL}s" >&2
		return
	fi
	chown root:root "$CERT.new" "$KEY.new"
	chmod 0644 "$CERT.new"
	chmod 0600 "$KEY.new"
	mv -f "$KEY.new" "$KEY" && mv -f "$CERT.new" "$CERT"
	PENDING=1
	reload "renewed certificate"
}

while true; do
	sleep "${INTERVAL}"
	refresh
done
