#!/bin/sh
# Container entrypoint (runs as root, PID 1 until it execs supervisord).
#
# The persistent root /data is a host bind mount (spec 9), so it arrives owned
# by the host user (typically root), not by the unprivileged panel user that
# actually writes the SQLite database, setup token and DKIM keys (spec 7.6.8).
# Fix its ownership here — the one place still running as root — before handing
# off to supervisord, which starts the panel as the panel user.
set -e

chown panel:panel /data
# Restored backups or previously-created state may contain panel-owned files
# under /data; make sure they stay writable without disturbing anything that a
# later phase deliberately hands to another service.
find /data -mindepth 1 -maxdepth 1 ! -user panel -exec chown -R panel:panel {} +

exec /usr/bin/supervisord -c /etc/supervisor/supervisord.conf
