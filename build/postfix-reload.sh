#!/bin/sh
# The reload the panel asks supervisord for (program postfix-reload,
# build/supervisord.conf; internal/supervisor RunOnce reads its exit status).
#
# Exit 75 says no Postfix master runs - it is stopped, or still in its start
# behind the milter wait (build/postfix-wrapper.sh) - and the master that
# starts next reads the files as they are now, so the panel takes that as
# done. Anything else is "postfix reload" itself: 0, or its failure (Postfix
# 3.7.11, conf/postfix-script, "reload": exit 1 when not running or when
# postsuper fails; src/postfix/postfix.c, a main.cf it cannot read). "master
# -t" exits 0, silently, while no master holds the lock (src/master/master.c,
# test_lock). A master that stops between the two is a failure the panel
# reports - the rarer mistake (review of stage 2c, F5).
d=$(postconf -h daemon_directory) || exit 1
if "$d/master" -t 2>/dev/null; then
	echo "postfix-reload: Postfix is not running; its start reads the files" >&2
	exit 75
fi
exec /usr/sbin/postfix reload
