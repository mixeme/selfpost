#!/bin/sh
# Postfix start wrapper: solves the cold-start race where Postfix would try to
# reach the milter sockets before they are listening.
#
# It blocks until the milter sockets — OpenDKIM, the panel's sender check and
# journal-milter and, with the inbound relay on, its inbound journal milter —
# are present,
# then execs `postfix start-fg`. If they are not ready within the
# timeout it exits non-zero WITHOUT starting Postfix, so supervisord/Docker see
# an explicit startup failure instead of a relay running blind.
#
# This handles cold start only. Runtime milter failures after a successful start
# are governed by fail-open (milter_default_action).
set -eu

OPENDKIM_SOCK="${OPENDKIM_SOCKET:-/run/opendkim/opendkim.sock}"
JOURNAL_SOCK="${JOURNAL_MILTER_SOCKET:-/run/selfpost/journal.sock}"
SENDER_SOCK="${SENDER_CHECK_SOCKET:-/run/selfpost/sender.sock}"
TIMEOUT="${MILTER_WAIT_TIMEOUT:-30}"
INTERVAL=1

SOCKS="$OPENDKIM_SOCK $JOURNAL_SOCK $SENDER_SOCK"
if [ "${INBOUND_RELAY_ENABLE:-false}" = "true" ]; then
	SOCKS="$SOCKS ${INBOUND_JOURNAL_MILTER_SOCKET:-/run/selfpost/inbound-journal.sock}"
fi

elapsed=0
for sock in $SOCKS; do
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

echo "postfix-wrapper: milter sockets ready, starting postfix"
exec postfix start-fg
