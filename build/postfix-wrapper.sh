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
# This handles cold start only. Once Postfix runs, a milter that does not
# answer costs what its default_action says (build/postfix-config.sh): mail is
# deferred for OpenDKIM and the sender check, and passes the journal-milter.
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
# Postfix 3.7.11: postfix(1) execs conf/postfix-script (src/postfix/postfix.c),
# whose "start-fg" execs master only as PID 1 and otherwise runs "master -s" as
# its child; master then makes itself a session leader (src/master/master.c,
# setsid()). The process supervisord watches is that shell, not master.
exec postfix start-fg
