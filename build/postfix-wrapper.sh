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
# Postfix 3.10.13: postfix(1) execs conf/postfix-script (src/postfix/postfix.c:669),
# whose "start-fg" (:200-210) execs master only as PID 1 and otherwise runs
# "master -s" as its child; master then makes itself a session leader
# (src/master/master.c:441, setsid()). A signal supervisord sends this
# program never reaches master
# (supervisor 4.2.5, supervisor/process.py, Subprocess.kill(): to the pid
# alone without stopasgroup, and master is not in its group anyway): before,
# a stop left master running - a restart of the program then found Postfix
# "already running", and a stop of the container cut Postfix off with
# SIGKILL (assumptions C-P42). So this shell stays, and on TERM or INT runs
# "postfix stop" (conf/postfix-script:237-253, "stop": TERM to master, then
# waits up to five seconds and kills its group; src/master/master_sig.c:205,
# master_sigdeath(): "terminating on signal 15"). Debian's redirect of
# "postfix start" to systemd (debian/patches/
# debian-re-run-startup-through-systemd.patch, conf/postfix-script:170-189)
# leaves start-fg alone and needs /run/systemd/system, which this container
# has not.
#
# Since Postfix 3.8 postfix(1) and postlog(1) - postfix-script's $INFO, $WARN
# and $FATAL - write their messages to stderr even when it is not a terminal
# (src/postfix/postfix.c:546, msg_vstream_init() no longer under isatty();
# RELEASE_NOTES-3.8:114-115, [Incompat 20230308]), besides maillog_file. So
# "starting the Postfix mail system", "stopping ..." and check-warn's
# warnings now reach the container's log through this program's stderr
# (build/supervisord.conf) as well as /data/log/mail.log; nothing reads them
# there but a person.
#
# "postfix start-fg" spends its first seconds in its checks before master
# runs; a stop asked then would find nothing to stop, so it is asked again
# until the child is gone. "master -t" exits 0, silently, while no master
# holds the lock (src/master/master.c:495-499, test_lock).
daemon_directory=$(postconf -h daemon_directory)
child=
stop_asked=
stop_postfix() {
	if [ -z "$child" ]; then
		# Between "&" and "child=$!": stopped right after the pid is known.
		stop_asked=1
		return
	fi
	trap '' TERM INT
	while kill -0 "$child" 2>/dev/null; do
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
if [ -n "$stop_asked" ]; then
	stop_postfix
fi
# A trapped signal makes wait return at once, the trap is taken right after
# (POSIX, Shell Command Language, 2.11 Signals and Error Handling) and exits.
# Otherwise wait returns when Postfix ended on its own: its status is this
# program's, and supervisord restarts it.
status=0
wait "$child" || status=$?
echo "postfix-wrapper: postfix exited with status $status" >&2
exit "$status"
