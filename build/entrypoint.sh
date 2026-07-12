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

# Application SASL accounts (spec 5.1, 9). The panel (user `panel`) writes the
# sasldb2 via saslpasswd2; Postfix (user `postfix`) reads it to authenticate SMTP
# clients. Share it through the `selfpost` group the same way as the DKIM tree:
# setgid directory so new files inherit the group, and the database itself
# group-readable (0640). Postfix wiring to actually consult it lands in Phase 5.
mkdir -p /data/sasl
chown -R panel:selfpost /data/sasl
chmod 2750 /data/sasl
[ -e /data/sasl/sasldb2 ] && chmod 0640 /data/sasl/sasldb2

# Postfix sender_login_maps (spec 5.1). The panel writes it; Postfix reads it.
# Ensure the file exists (empty is fine) before Postfix starts so a reload that
# references it never fails on a missing file, and keep it group-readable.
mkdir -p /data/postfix
[ -e /data/postfix/sender_login_maps ] || : > /data/postfix/sender_login_maps
chown -R panel:selfpost /data/postfix
chmod 2750 /data/postfix
chmod 0640 /data/postfix/sender_login_maps

# Milter socket directories (spec 5 p.3, 7.3). From Phase 5 Postfix (user
# `postfix`) must actually CONNECT to both milter sockets — OpenDKIM's and the
# panel's journal-milter — not just probe them at start-up. The sockets are
# created by the opendkim and panel users respectively, so bridge them to
# `postfix` through the shared `selfpost` group: group-owned + setgid dirs mean
# each socket created inside inherits group `selfpost`, and group-traversable
# (2750) lets postfix reach it. Without this, smtpd cannot talk to OpenDKIM and,
# because signing is strict (default_action=tempfail), rejects all mail.
mkdir -p /run/opendkim /run/selfpost
chown opendkim:selfpost /run/opendkim
chown panel:selfpost /run/selfpost
chmod 2750 /run/opendkim /run/selfpost

# Generate the outbound-relay Postfix configuration from the environment (spec
# 5). Kept out of the image build so cert paths, rate limits, hostname and the
# optional 587 service are all driven by env at run time, and re-derived on every
# start the same way the /data normalisation above is.
/usr/local/bin/postfix-config.sh

exec /usr/bin/supervisord -c /etc/supervisor/supervisord.conf
