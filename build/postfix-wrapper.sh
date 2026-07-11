#!/bin/sh
# Postfix start wrapper (spec 4): solves the cold-start race where Postfix would
# try to reach the milter sockets before they are listening.
#
# It blocks until BOTH milter sockets — OpenDKIM and the panel's journal-milter
# — are present, then execs `postfix start-fg`. If they are not ready within the
# timeout it exits non-zero WITHOUT starting Postfix, so supervisord/Docker see
# an explicit startup failure instead of a relay running blind.
#
# This handles cold start only. Runtime milter failures after a successful start
# are governed by fail-open (milter_default_action), configured in Phase 5.
set -eu

OPENDKIM_SOCK="${OPENDKIM_SOCKET:-/run/opendkim/opendkim.sock}"
JOURNAL_SOCK="${JOURNAL_MILTER_SOCKET:-/run/selfpost/journal.sock}"
TIMEOUT="${MILTER_WAIT_TIMEOUT:-30}"
INTERVAL=1

elapsed=0
for sock in "$OPENDKIM_SOCK" "$JOURNAL_SOCK"; do
	while [ ! -S "$sock" ]; do
		if [ "$elapsed" -ge "$TIMEOUT" ]; then
			echo "postfix-wrapper: timed out after ${TIMEOUT}s waiting for milter socket $sock" >&2
			exit 1
		fi
		sleep "$INTERVAL"
		elapsed=$((elapsed + INTERVAL))
	done
	echo "postfix-wrapper: milter socket ready: $sock"
done

echo "postfix-wrapper: both milter sockets ready, starting postfix"
exec postfix start-fg
