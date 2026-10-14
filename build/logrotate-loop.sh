#!/bin/sh
# Periodic logrotate for /data/log/mail.log. Rotation renames the file,
# recreates it (`create 0640 postfix selfpost`, matching a cold container
# start), then runs `postfix reload` (the same mechanism `postfix logrotate`
# uses): postlogd keeps writing to the renamed inode until reload, and the
# panel's log-tailer holds its own descriptor on that inode, so nothing written
# before the reload is lost. `create` (rather than `nocreate`) matters here
# beyond timing: a postlogd-triggered recreate lands the file at 0600 owned by
# postfix, which the unprivileged panel process cannot read — confirmed on a
# live container (date not recorded), and so in the source: Postfix 3.7.11,
# src/util/logwriter.c, logwriter_open_or_die(), creates it 0644 under master's
# umask(077), src/master/master.c — so logrotate must be the one to create it.
# The reload: Postfix 3.7.11, conf/postfix-script, "logrotate" (mv, then kill
# -HUP master); master_conf.c, master_config() -> master_restart_service() ->
# master_status_cleanup(), on which postlogd exits (src/master/dgram_server.c,
# dgram_server_abort() on MASTER_STATUS_FD), and the next postlogd opens the
# path anew (src/postlogd/postlogd.c, logwriter_open_or_die()).
#
# logrotate itself only rotates once the configured "daily" period has elapsed
# (tracked in /var/lib/logrotate/status), so it is safe to invoke this more
# often than daily — polling merely bounds how late a legitimate rotation runs.
# (logrotate 3.21.0-1: debian/rules, --with-state-file-path=/var/lib/logrotate/
# status. That file is in the container's own filesystem: a recreated container
# starts without it, and logrotate.c, newState(), records a log it has no state
# for as rotated today, so the first rotation after a recreate comes a day
# later.)
set -eu

CONFIG=/etc/logrotate.d/mail
INTERVAL="${LOGROTATE_INTERVAL_SECONDS:-21600}"

# logrotate refuses configs writable by group or others and exits 0 while
# ignoring them — fail here so supervisord reports the fault. (logrotate
# 3.21.0, config.c, readConfigFile(): as root, mode & 0022 gives "Ignoring ...
# because it is writable by group or others." and returns 0, not an error; a
# config not owned by root is ignored the same way. "Potentially dangerous
# mode" is only a warning there, for any mode & 07533 other than 0400 - this
# loop chooses to treat it as fatal too.)
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
