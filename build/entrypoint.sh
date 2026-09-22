#!/bin/sh
# Container entrypoint (runs as root, PID 1 until it execs supervisord).
#
# SELFPOST_HOSTNAME is checked first (plan B.3): it must fail fast with a clear
# message before /data normalisation or Postfix config, so a bad identity never
# looks like a permissions or packaging problem (and so the e2e hostname-gate
# tests see the FATAL text rather than an earlier set -e abort).
set -e

# SELFPOST_HOSTNAME is an identity, not a setting with a safe default: it must
# simultaneously match the PTR/rDNS record, the certificate CN/SAN, and the
# Cyrus SASL realm (spec 5.2 p.3, 8). The panel (main.go saslRealm()) and
# postfix-config.sh each fall back independently when it's unset — to
# `localhost` and to the container hostname respectively — so accounts get
# written under one realm and looked up under another and authentication
# silently fails for every application, while HELO also stops matching the
# PTR record and mail that does go out lands in spam. No fallback can be
# correct, so fail loudly here, before either side of that split has a chance
# to run, rather than leave a green panel with broken mail.
if [ -z "$SELFPOST_HOSTNAME" ]; then
	cat >&2 <<'EOF'
FATAL: SELFPOST_HOSTNAME is not set.

This is the mail server's identity: it becomes the Postfix HELO/EHLO name,
the Cyrus SASL realm that application passwords are looked up under, and it
must match the TLS certificate's CN/SAN as well as this server's PTR (reverse
DNS) record. There is no safe default — guessing any one of these wrong
breaks authentication for every application or sends outgoing mail to spam,
silently.

Set it to the mail server's fully-qualified domain name, e.g.:

    SELFPOST_HOSTNAME=mail.example.com

in the .env file next to your docker-compose.yml (see deploy/.env.example).
EOF
	exit 1
fi

case "$SELFPOST_HOSTNAME" in
	*[\ \	]* | *://* | *:* )
		echo "FATAL: SELFPOST_HOSTNAME must be a bare hostname (no scheme, port, or spaces): \"$SELFPOST_HOSTNAME\"" >&2
		echo 'Example: SELFPOST_HOSTNAME=mail.example.com' >&2
		exit 1
		;;
	*.*)
		;;
	*)
		echo "FATAL: SELFPOST_HOSTNAME must be a fully-qualified domain name (at least one dot): \"$SELFPOST_HOSTNAME\"" >&2
		echo 'Example: SELFPOST_HOSTNAME=mail.example.com' >&2
		exit 1
		;;
esac

# The persistent root /data is a host bind mount (spec 9), so it arrives owned
# by the host user (typically root), not by the unprivileged panel user that
# actually writes the SQLite database, setup token and DKIM keys (spec 7.6.8).
# Fix its ownership here — the one place still running as root — before handing
# off to supervisord, which starts the panel as the panel user.
#
# Mode must stay world-traversable (0755): OpenDKIM and Postfix reach their
# trees under /data as other users. Go's testing.TempDir is 0700, and a bare
# chown would leave that mode in place — opendkim then cannot read KeyTable and
# the container crash-loops (e2e TestHostnameGate/valid_hostname_starts).
chown panel:panel /data
chmod 755 /data
# Restored backups or previously-created state may contain panel-owned files
# under /data; make sure they stay writable without disturbing anything that a
# later phase deliberately hands to another service. /data/log is exempt: it is
# deliberately owned by postfix (postlogd writes the delivery log there) and is
# normalised on its own below.
find /data -mindepth 1 -maxdepth 1 ! -user panel ! -name log ! -name postfix -exec chown -R panel:panel {} +

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
# group-readable (0640).
mkdir -p /data/sasl
chown -R panel:selfpost /data/sasl
chmod 2750 /data/sasl
[ -e /data/sasl/sasldb2 ] && chmod 0640 /data/sasl/sasldb2

# Postfix state under /data (spec 5.1, architecture.md § Persistence). The panel
# writes sender_login_maps; Postfix owns the on-disk queue tree under queue/.
mkdir -p /data/postfix/queue
[ -e /data/postfix/sender_login_maps ] || : > /data/postfix/sender_login_maps
chown panel:selfpost /data/postfix
chmod 2750 /data/postfix
chown panel:selfpost /data/postfix/sender_login_maps
chmod 0640 /data/postfix/sender_login_maps

# Delivery log (architecture.md § Log tailer). postlogd writes it as user
# `postfix`; the panel reads it for the log-tailer and the System log page. It
# lives under /data — not the ephemeral /var/log — so the delivery lines that
# resolve a "queued" send-log row survive a container recreate.
#
# postlogd creates a missing log itself, but at 0600, which the unprivileged
# panel cannot read; so create it here (and re-normalise an existing one, plus
# whatever logrotate left behind) at 0640 owned postfix:selfpost. The setgid
# directory keeps the shared group on anything created inside it later, and
# 2750 keeps it group-traversable but not group-writable — logrotate refuses to
# rotate a log whose directory is writable by a non-root group.
mkdir -p /data/log
[ -e /data/log/mail.log ] || : > /data/log/mail.log
chown -R postfix:selfpost /data/log
chmod 2750 /data/log
find /data/log -type f -exec chmod 0640 {} +

# Milter socket directories (spec 5 p.3, 7.3). Postfix (user `postfix`) must
# actually CONNECT to both milter sockets — OpenDKIM's and the panel's
# journal-milter — not just probe them at start-up. The sockets are
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

# Initialise the persistent queue tree on first start or after restore. postfix
# set-permissions reads queue_directory from main.cf (set by postfix-config.sh).
if [ ! -d /data/postfix/queue/active ]; then
	postfix set-permissions
fi
chown -R postfix:postfix /data/postfix/queue

exec /usr/bin/supervisord -c /etc/supervisor/supervisord.conf
