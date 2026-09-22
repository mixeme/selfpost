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

INTERVAL="${LOGROTATE_INTERVAL_SECONDS:-21600}"

while true; do
	if logrotate /etc/logrotate.d/mail; then
		:
	else
		echo "logrotate-loop: logrotate failed, will retry after ${INTERVAL}s" >&2
	fi
	sleep "${INTERVAL}"
done
