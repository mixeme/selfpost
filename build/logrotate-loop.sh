#!/bin/sh
# Periodic logrotate for /var/log/mail.log (spec 9, 10). Postfix's maillog_file
# is written by postlogd, which keeps the file open for the life of the
# process — there is no daemon to signal on rotation, so the logrotate.d config
# uses copytruncate (a brief truncation race can drop the last few in-flight
# lines, which is an acceptable trade for not having to reload Postfix on every
# rotation).
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
