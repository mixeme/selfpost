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

# DKIM key tree (spec 6, 9). The panel (user `panel`) generates keys and writes
# the OpenDKIM tables; OpenDKIM (user `opendkim`) must read them. Normalise the
# tree on every start so it is correct whether /data is fresh, restarted, or
# just restored from a backup:
#   - group `selfpost` + setgid on directories so anything the panel creates
#     inherits the shared group OpenDKIM traverses;
#   - private keys and tables group-readable (0640);
#   - both table files present (empty is fine) BEFORE OpenDKIM starts, so the
#     daemon comes up cleanly with no domains yet.
mkdir -p /data/opendkim/keys
for t in /data/opendkim/KeyTable /data/opendkim/SigningTable; do
	[ -e "$t" ] || : > "$t"
done
chown -R panel:selfpost /data/opendkim
find /data/opendkim -type d -exec chmod 2750 {} +
chmod 0640 /data/opendkim/KeyTable /data/opendkim/SigningTable
find /data/opendkim/keys -type f -name '*.private' -exec chmod 0640 {} +

exec /usr/bin/supervisord -c /etc/supervisor/supervisord.conf
