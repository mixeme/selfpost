#!/bin/sh
# Periodic logrotate for /data/log/mail.log. Rotation renames the file,
# recreates it (`create 0640 postfix selfpost`, matching a cold container
# start), then runs `postfix reload` (the same mechanism `postfix logrotate`
# uses): postlogd keeps writing to the renamed inode until reload, and the
# panel's log-tailer holds its own descriptor on that inode, so nothing written
# before the reload is lost. `create` (rather than `nocreate`) matters here
# beyond timing: a postlogd-triggered recreate lands the file at 0600 owned by
# postfix, which the unprivileged panel process cannot read — confirmed on a
# live container (date not recorded, Postfix 3.7.11), and so in the source:
# Postfix 3.10.13, src/util/logwriter.c:99, logwriter_open_or_die(), creates it
# with logwriter_perms, 0600 (:86) unless maillog_file_permissions says
# otherwise (Postfix 3.9 and later). build/postfix-config.sh now sets that to
# 0640, so a file postlogd makes is readable too; logrotate still creates it,
# so that the new log is there, with its mode and owner, at once.
# The reload: Postfix 3.10.13, conf/postfix-script:535-566, "logrotate" (mv,
# then kill -HUP master); master_conf.c:92, master_config() -> :147,
# master_restart_service() -> master_status_cleanup(), on which postlogd exits
# (src/master/dgram_server.c:241, dgram_server_abort(), :640 on
# MASTER_STATUS_FD), and the next postlogd opens the path anew
# (src/postlogd/postlogd.c:236, logwriter_open_or_die()).
#
# logrotate itself only rotates once the configured "daily" period has elapsed
# (tracked in its state file), so it is safe to invoke this more often than
# daily — polling merely bounds how late a legitimate rotation runs. The state
# file is beside the log, under /data, not at the default
# /var/lib/logrotate/status (logrotate 3.22.0-1: debian/rules:11,
# --with-state-file-path) in the container's own filesystem: a recreated
# container started without it, logrotate.c:456, newState(), records a log it
# has no state for as rotated today, and so a host that recreates the container
# daily - an .env change, an image bump - never rotated mail.log at all (audit
# 2026-10-12, M19). (logrotate 3.22.0, logrotate.8.in:74: "-s, --state
# statefile".) The directory is postfix:selfpost 2750 and logrotate runs as
# root.
set -eu

CONFIG=/etc/logrotate.d/mail
STATE=/data/log/.logrotate.status
INTERVAL="${LOGROTATE_INTERVAL_SECONDS:-21600}"

# logrotate refuses configs writable by group or others and goes on without
# them — fail here so supervisord reports the fault. (logrotate 3.22.0,
# config.c:1099, readConfigFile(): as root, mode & 0022 gives (:1160-1165)
# "Ignoring ... because it is writable by group or others." and returns 1; a
# config not owned by root is ignored the same way (:1168-1173). Since 3.22.0
# that makes logrotate's exit status 1 (logrotate.c:3278-3279 and :3301;
# ChangeLog.md, 3.22.0: "in case a configuration file was skipped due to
# unsafe permissions the exit status after rotattion will be `1` (#508)");
# 3.21.0 returned 0 there and exited 0. So the output is looked at whatever
# the status. "Potentially dangerous mode" is only a warning there
# (:1154-1157), for any mode & 07533 other than 0400 - this loop chooses to
# treat it as fatal too.)
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
	status=0
	out=$(logrotate -s "$STATE" "$CONFIG" 2>&1) || status=$?
	case "$out" in
		*Ignoring*|*Potentially\ dangerous\ mode*)
			echo "$out" >&2
			echo "logrotate-loop: refusing to run: logrotate did not take $CONFIG as it is (above)" >&2
			exit 1
			;;
	esac
	if [ "$status" -ne 0 ]; then
		echo "$out" >&2
		return 1
	fi
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
