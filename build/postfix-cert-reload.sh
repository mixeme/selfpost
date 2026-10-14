#!/bin/sh
# Picks up a renewed TLS certificate while the container runs (spec 5.2 p.4;
# audit 2026-10-12, H2).
#
# Postfix does not read the mounted files: postfix-config.sh copies them at
# start into /etc/postfix/tls-internal, because Postfix wants the key owned by
# root with mode 0600, which a read-only bind mount owned by the host's user
# cannot give. A renewal of the mounted files therefore reaches Postfix only
# through a new copy. Every interval this compares the mounted files with the
# copy; when they differ, and the certificate and the key belong together (a
# renewer writes them one after the other), it replaces the copy - each file
# written beside its target and renamed over it, with the owner and modes of
# the start - and reloads Postfix. When nothing changed it does nothing.
#
# Runs under supervisord as root. It sleeps first: at container start the
# wrapper is still bringing Postfix up. It never exits non-zero, so a failure
# does not trip the crashexit listener; it is logged and tried again.
set -u

INTERVAL="${TLS_RELOAD_INTERVAL_SECONDS:-86400}"
SRC_CERT="${TLS_CERT_FILE:-/etc/postfix/tls/fullchain.pem}"
SRC_KEY="${TLS_KEY_FILE:-/etc/postfix/tls/privkey.pem}"
DIR=/etc/postfix/tls-internal

reload() {
	if postfix reload; then
		echo "cert-reload: postfix reloaded ($1)"
	else
		echo "cert-reload: postfix reload failed, will retry after ${INTERVAL}s" >&2
	fi
}

# same_pair says whether a certificate and a private key belong together.
same_pair() {
	cert_pub=$(openssl x509 -noout -pubkey -in "$1" 2>/dev/null) || return 1
	key_pub=$(openssl pkey -pubout -in "$2" 2>/dev/null) || return 1
	[ -n "$cert_pub" ] && [ "$cert_pub" = "$key_pub" ]
}

# install_copy SRC DST MODE: DST becomes a root-owned copy of SRC in one rename.
install_copy() {
	cp -f "$1" "$2.new" && chown root:root "$2.new" && chmod "$3" "$2.new" && mv -f "$2.new" "$2"
}

refresh() {
	if [ ! -f "$DIR/fullchain.pem" ]; then
		# No copy: the mounted files were missing at start and Postfix was
		# pointed at them as they are. A reload is what picks up a change there.
		reload "periodic TLS refresh"
		return
	fi
	if cmp -s "$SRC_CERT" "$DIR/fullchain.pem" && cmp -s "$SRC_KEY" "$DIR/privkey.pem"; then
		return
	fi
	if ! same_pair "$SRC_CERT" "$SRC_KEY"; then
		echo "cert-reload: the mounted certificate and key differ from Postfix's copy but do not belong together; will look again after ${INTERVAL}s" >&2
		return
	fi
	if install_copy "$SRC_KEY" "$DIR/privkey.pem" 0600 && install_copy "$SRC_CERT" "$DIR/fullchain.pem" 0644; then
		reload "renewed certificate"
	else
		echo "cert-reload: copying the renewed certificate failed, will retry after ${INTERVAL}s" >&2
	fi
}

while true; do
	sleep "${INTERVAL}"
	refresh
done
