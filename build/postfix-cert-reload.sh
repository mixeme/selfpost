#!/bin/sh
# Periodic `postfix reload` so refreshed TLS certificates are picked up (spec
# 5.2 p.4). The reverse-proxy renews the PEM files in the read-only mount every
# few months; Postfix only re-reads them on reload. A simple daily reload is
# more than enough (a day of staleness is harmless) and far simpler than an
# inotify watcher — the spec explicitly prefers this.
#
# Runs under supervisord as root, so it can reload Postfix directly. It sleeps
# first, then reloads in a loop: no reload at container start (the wrapper is
# still bringing Postfix up then) and none until at least one interval has
# passed. A reload is harmless when nothing changed.
set -eu

INTERVAL="${TLS_RELOAD_INTERVAL_SECONDS:-86400}"

while true; do
	sleep "${INTERVAL}"
	if postfix reload; then
		echo "cert-reload: postfix reloaded (periodic TLS refresh)"
	else
		# Never exit non-zero: a transient reload failure must not trip the
		# crashexit listener and take the container down. Log and retry next cycle.
		echo "cert-reload: postfix reload failed, will retry after ${INTERVAL}s" >&2
	fi
done
