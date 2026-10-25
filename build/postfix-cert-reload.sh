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
# and reloads Postfix. When nothing changed it does nothing. When the files
# were not there at start, so that no copy was made, it looks every minute
# and makes the copy once they are (adopt).
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
SRC_CERT="${TLS_CERT_FILE:-/etc/selfpost/tls/fullchain.pem}"
SRC_KEY="${TLS_KEY_FILE:-/etc/selfpost/tls/privkey.pem}"
DIR=/etc/postfix/tls-internal
CERT="$DIR/fullchain.pem"
KEY="$DIR/privkey.pem"
PENDING=0 # a copy was renewed and Postfix has not been reloaded since

# How often to look while there is no copy: the certificate was not there at
# start. A proxy that obtains its certificate itself (Caddy, Traefik) has it
# only after SelfPost has started, and until then 465 and 587 have no TLS -
# measured on a fresh host, 2026-10-25: Caddy had it 9 s after its start.
# A minute, or the interval when that is shorter.
LOOK=60
case "$INTERVAL" in
'' | *[!0-9]*) ;;
*) [ "$INTERVAL" -lt "$LOOK" ] && LOOK=$INTERVAL ;;
esac

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

# copied says whether Postfix serves a copy, as it does when the certificate
# was there at start (postfix-config.sh) or appeared later (adopt below).
copied() {
	[ "$(postconf -h smtpd_tls_cert_file 2>/dev/null)" = "$CERT" ]
}

# copy_pair copies the mounted pair beside Postfix's copy, checks that the
# copies - not the mounted files, which a renewer may be rewriting - are a
# certificate and the key that belongs to it, and renames both over the
# copy, the key root's alone. $1 names the pair in the messages.
copy_pair() {
	# 0755 as postfix-config.sh makes it at the start, not this script's
	# umask's 0700: the panel reads the certificate through it for Health.
	# A container that started without a certificate has no such folder yet
	# (measured behind Caddy on a fresh host, 2026-10-25: Health "could not
	# read" the copy).
	mkdir -p "$DIR" && chmod 0755 "$DIR"
	rm -f "$CERT.new" "$KEY.new"
	if ! cp "$SRC_CERT" "$CERT.new" || ! cp "$SRC_KEY" "$KEY.new"; then
		rm -f "$CERT.new" "$KEY.new"
		echo "cert-reload: copying the $1 failed, will try again after $2s" >&2
		return 1
	fi
	if ! same_pair "$CERT.new" "$KEY.new"; then
		rm -f "$CERT.new" "$KEY.new"
		echo "cert-reload: the $1 and its key do not belong together; will look again after $2s" >&2
		return 1
	fi
	chown root:root "$CERT.new" "$KEY.new"
	chmod 0644 "$CERT.new"
	chmod 0600 "$KEY.new"
	mv -f "$KEY.new" "$KEY" && mv -f "$CERT.new" "$CERT"
}

# adopt makes the copy the start would have made, once the certificate that
# was missing then is there whole, and points Postfix at it. Until 2.16.1
# Postfix went on reading the mounted files: each new smtpd took the
# certificate once it was there, but the panel, which reads what Postfix
# serves, could not read a key the proxy keeps root's alone, and Health
# called the certificate unreadable until a restart.
adopt() {
	if [ ! -r "$SRC_CERT" ] || [ ! -r "$SRC_KEY" ]; then
		return
	fi
	copy_pair "certificate that appeared after start" "$LOOK" || return
	if ! postconf -e "smtpd_tls_cert_file=$CERT" "smtpd_tls_key_file=$KEY"; then
		echo "cert-reload: pointing Postfix at the copy failed, will try again after ${LOOK}s" >&2
		return
	fi
	PENDING=1
	reload "certificate appeared after start"
}

refresh() {
	if [ "$PENDING" = 1 ]; then
		reload "renewed certificate, earlier reload had failed"
	fi
	if ! copied; then
		adopt
		return
	fi
	if [ ! -r "$SRC_CERT" ] || [ ! -r "$SRC_KEY" ]; then
		echo "cert-reload: cannot read ${SRC_CERT} or ${SRC_KEY}; Postfix keeps the certificate it has" >&2
		return
	fi
	if cmp -s "$SRC_CERT" "$CERT" && cmp -s "$SRC_KEY" "$KEY"; then
		return
	fi
	copy_pair "renewed certificate" "$INTERVAL" || return
	PENDING=1
	reload "renewed certificate"
}

while true; do
	if copied; then
		sleep "${INTERVAL}"
	else
		sleep "${LOOK}"
	fi
	refresh
done
