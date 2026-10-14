#!/bin/sh
# Postfix start wrapper: solves the cold-start race where Postfix would try to
# reach the milter sockets before they are listening.
#
# It blocks until the milter sockets — OpenDKIM, the panel's sender check and
# journal-milter and, with the inbound relay on, its inbound journal milter —
# are present, then runs `postfix start-fg` and stays its parent. If they are
# not ready within the timeout it exits non-zero WITHOUT starting Postfix.
# supervisord counts that exit as one that came too quickly - the program's
# startsecs is the timeout plus ten seconds (build/entrypoint.sh) - so after
# startretries it is FATAL and crashexit takes the container down, instead of
# a relay running blind (supervisor 4.2.5, supervisor/process.py,
# Subprocess.finish(): an exit before startsecs is "Exited too quickly" and
# goes to BACKOFF; transition(): BACKOFF past startretries is FATAL;
# assumptions C-V6).
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
# setsid()). A signal supervisord sends this program never reaches master
# (supervisor 4.2.5, supervisor/process.py, Subprocess.kill(): to the pid
# alone without stopasgroup, and master is not in its group anyway): before,
# a stop left master running - a restart of the program then found Postfix
# "already running", and a stop of the container cut Postfix off with
# SIGKILL (assumptions C-P42). So this shell stays, and on TERM or INT runs
# "postfix stop" (conf/postfix-script, "stop": TERM to master, then waits up
# to five seconds and kills its group; src/master/master_sig.c,
# master_sigdeath(): "terminating on signal 15").
#
# "postfix start-fg" spends its first seconds in its checks before master
# runs; a stop asked then would find nothing to stop, so it is asked again
# until the child is gone. "master -t" exits 0, silently, while no master
# holds the lock (src/master/master.c, test_lock).
daemon_directory=$(postconf -h daemon_directory)
child=
stop_postfix() {
	trap '' TERM INT
	while [ -n "$child" ] && kill -0 "$child" 2>/dev/null; do
		if "$daemon_directory/master" -t 2>/dev/null; then
			sleep 1
		else
			postfix stop || true
		fi
	done
	exit 0
}
trap stop_postfix TERM INT
postfix start-fg &
child=$!
# A trapped signal makes wait return at once, the trap is taken right after
# (POSIX, Shell Command Language, 2.11 Signals and Error Handling) and exits.
# Otherwise wait returns when Postfix ended on its own: its status is this
# program's, and supervisord restarts it.
status=0
wait "$child" || status=$?
echo "postfix-wrapper: postfix exited with status $status" >&2
exit "$status"
