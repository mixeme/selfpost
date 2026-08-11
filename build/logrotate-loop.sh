#!/bin/sh
# Periodic logrotate for /data/log/mail.log (spec 9, 10). Rotation renames the
# file, recreates it (`create 0640 postfix selfpost`, matching a cold container
# start), then runs `postfix reload` (the same mechanism `postfix logrotate`
# uses): postlogd keeps writing to the renamed inode until reload, and the
# panel's log-tailer holds its own descriptor on that inode, so nothing
# written before the reload is lost. `create` (rather than `nocreate`) matters
# here beyond timing: a postlogd-triggered recreate lands the file at 0600
# owned by postfix, which the unprivileged panel process cannot read —
# confirmed on a live container — so logrotate must be the one to create it.
#
# logrotate itself only rotates once the configured "daily" period has elapsed
# (tracked in /var/lib/logrotate/status), so it is safe to invoke this more
# often than daily — polling merely bounds how late a legitimate rotation runs.
set -eu

CONFIG=/etc/logrotate.d/mail
INTERVAL="${LOGROTATE_INTERVAL_SECONDS:-21600}"

# logrotate refuses configs writable by group or others and exits 0 while
# ignoring them — fail here so supervisord reports the fault.
logrotate_config_ok() {
	mode=$(stat -c '%a' "$CONFIG")
	mode=${mode#0}
	grp=$(( (mode / 10) % 10 ))
	oth=$(( mode % 10 ))
	case $grp in 2|3|6|7) return 1 ;; esac
	case $oth in 2|3|6|7) return 1 ;; esac
	return 0
}

logrotate_config_fatal() {
	echo "logrotate-loop: refusing to run: $CONFIG mode $(stat -c '%a' "$CONFIG") is writable by group or others" >&2
	exit 1
}

if ! logrotate_config_ok; then
	logrotate_config_fatal
fi

run_logrotate() {
	out=$(logrotate "$CONFIG" 2>&1) || {
		echo "$out" >&2
		return 1
	}
	case "$out" in
		*Ignoring*|*Potentially\ dangerous\ mode*)
			echo "$out" >&2
			logrotate_config_fatal
			;;
	esac
	return 0
}

while true; do
	if ! logrotate_config_ok; then
		logrotate_config_fatal
	fi
	if run_logrotate; then
		:
	else
		echo "logrotate-loop: logrotate failed, will retry after ${INTERVAL}s" >&2
	fi
	sleep "${INTERVAL}"
done
