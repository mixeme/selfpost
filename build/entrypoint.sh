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
# Cyrus SASL realm. The panel (main.go saslRealm()) and postfix-config.sh each
# fall back independently when it's unset — to `localhost` and to the container
# hostname respectively — so accounts get written under one realm and looked up
# under another and authentication silently fails for every application, while
# HELO also stops matching the PTR record and mail that does go out lands in
# spam. No fallback can be correct, so fail loudly here, before either side of
# that split has a chance to run, rather than leave a green panel with broken
# mail.
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

# The persistent root /data is a host bind mount, so it arrives owned by the
# host user (typically root), not by the unprivileged panel user that actually
# writes the SQLite database, setup token and DKIM keys. Fix its ownership here
# — the one place still running as root — before handing off to supervisord,
# which starts the panel as the panel user.
#
# Mode must stay world-traversable (0755): OpenDKIM and Postfix reach their
# trees under /data as other users. Go's testing.TempDir is 0700, and a bare
# chown would leave that mode in place — opendkim then cannot read KeyTable and
# the container crash-loops (e2e TestHostnameGate/valid_hostname_starts).
# (Go: testing.TempDir uses os.MkdirTemp, src/os/tempfile.go: "created with
# mode 0o700".)
chown panel:panel /data
chmod 755 /data
# Restored backups or previously-created state may contain panel-owned files
# under /data; make sure they stay writable without disturbing anything that a
# later phase deliberately hands to another service. /data/log is exempt: it is
# deliberately owned by postfix (postlogd writes the delivery log there) and is
# normalised on its own below.
find /data -mindepth 1 -maxdepth 1 ! -user panel ! -name log ! -name postfix -exec chown -R panel:panel {} +

# DKIM key tree. The panel (user `panel`) generates keys and writes
# the OpenDKIM tables; OpenDKIM (user `opendkim`) must read them. Normalise the
# tree on every start so it is correct whether /data is fresh, restarted, or
# just restored from a backup:
#   - group `selfpost` + setgid on directories so anything the panel creates
#     inherits the shared group OpenDKIM traverses (inode(7), "The
#     set-group-ID bit": files created in such a directory take its group);
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

# Application SASL accounts. The panel (user `panel`) writes the
# sasldb2 via saslpasswd2; Postfix (user `postfix`) reads it to authenticate SMTP
# clients. Share it through the `selfpost` group the same way as the DKIM tree:
# setgid directory so new files inherit the group, and the database itself
# group-readable (0640).
mkdir -p /data/sasl
chown -R panel:selfpost /data/sasl
chmod 2750 /data/sasl
[ -e /data/sasl/sasldb2 ] && chmod 0640 /data/sasl/sasldb2

# Postfix state under /data (architecture.md § Persistence). The panel writes
# sender_login_maps; Postfix owns the on-disk queue tree under queue/.
mkdir -p /data/postfix/queue
[ -e /data/postfix/sender_login_maps ] || : > /data/postfix/sender_login_maps
# Inbound relay maps (written by the panel when INBOUND_RELAY_ENABLE=true).
# Empty files keep postfix check happy if the flag is on before any domain exists.
for f in relay_domains transport relay_recipients tls_policy; do
	[ -e "/data/postfix/$f" ] || : > "/data/postfix/$f"
	chown panel:selfpost "/data/postfix/$f"
	chmod 0640 "/data/postfix/$f"
done
chown panel:selfpost /data/postfix
chmod 2750 /data/postfix

# Filter lists for the external spam filter (written by the panel when
# INBOUND_RELAY_ENABLE=true, read by the antispam sidecar, which mounts this
# directory). The sidecar runs under a user of its own in another container,
# so the directory and its files are readable by anyone: they hold sender
# addresses and networks, no secrets.
mkdir -p /data/antispam
chown panel:selfpost /data/antispam
chmod 0755 /data/antispam
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
# Postfix 3.7.11: src/util/logwriter.c, logwriter_open_or_die(), creates the
# file 0644, under master's umask(077) (src/master/master.c) - 0600. logrotate
# 3.21.0, logrotate.c, findNeedRotating(): "skipping ... because parent
# directory has insecure permissions" when the directory's group is not root
# and has write permission.
mkdir -p /data/log
[ -e /data/log/mail.log ] || : > /data/log/mail.log
chown -R postfix:selfpost /data/log
chmod 2750 /data/log
find /data/log -type f -exec chmod 0640 {} +

# Milter socket directories. Postfix (user `postfix`) must actually CONNECT to
# the milter sockets — OpenDKIM's and the panel's — not just probe them at
# start-up. The sockets are created by the opendkim and panel
# users respectively, so bridge them to `postfix` through the shared `selfpost`
# group: group-owned + setgid dirs mean each socket created inside inherits
# group `selfpost`, and group-traversable (2750) lets postfix reach it. Without
# this, smtpd cannot talk to OpenDKIM and, because signing is strict
# (default_action=tempfail), rejects all mail.
mkdir -p /run/opendkim /run/selfpost
chown opendkim:selfpost /run/opendkim
chown panel:selfpost /run/selfpost
chmod 2750 /run/opendkim /run/selfpost
# A container that was killed leaves its sockets behind, and a file nobody
# listens on would satisfy postfix-wrapper.sh's wait: Postfix would start
# before the sender check does and defer mail until it is there. The check's
# alive mark goes too: it tells the downtime of the panel restarting inside a
# running container, and a restart of the whole container is not the check's
# downtime (guide § Health) - a mark left from before would count the whole
# outage when supervisord cannot say since when Postfix runs.
# (test(1), -S: true for any socket file, whether or not anything listens.)
rm -f /run/opendkim/*.sock /run/selfpost/*.sock /run/selfpost/*.alive

# Generate the outbound-relay Postfix configuration from the environment (spec
# 5). Kept out of the image build so cert paths, rate limits, hostname and the
# optional 587 service are all driven by env at run time, and re-derived on every
# start the same way the /data normalisation above is.
/usr/local/bin/postfix-config.sh

# The queue tree, on a first start or after a restore, is already built here:
# postfix-config.sh ends with "postfix check", whose check-fatal runs
# "post-install create-missing" (Postfix 3.7.11, conf/postfix-script;
# conf/postfix-files, "$queue_directory/active:d:$mail_owner:-:700:ucr").
# A "postfix set-permissions" for a missing active/ stood here and never ran
# (assumptions C-P34).
chown -R postfix:postfix /data/postfix/queue
# The recursive chown above would leave public/ and maildrop/ in group postfix.
# Postfix keeps those two in its setgid group: postqueue and postdrop are setgid
# postdrop and reach the showq socket and the maildrop directory through that
# group alone. With the wrong group `postqueue -p` run by the panel answers
# "Connect to the Postfix showq service: Permission denied" and the panel cannot
# read the queue (the Queue card and the Outbound queue page).
chgrp postdrop /data/postfix/queue/public /data/postfix/queue/maildrop
# It would also leave the queue directory itself and pid/ owned by postfix.
# Postfix keeps those two owned by root (master, which runs as root, is the only
# writer) and logs "warning: not owned by root" for each at every start.
# (Postfix 3.7.11, conf/postfix-files: $queue_directory root 755, maildrop
# $setgid_group 730, public $setgid_group 710, pid root 755, the other queue
# directories $mail_owner 700; conf/postfix-script, "check-warn": "not owned by
# root".)
chown root /data/postfix/queue /data/postfix/queue/pid

# [program:postfix]'s startsecs (build/supervisord.conf): longer than the
# wrapper's wait for the milter sockets, so its exit at the end of the wait is
# one supervisord counts as too quick. A value that is not a number stops the
# start here.
wait_timeout="${MILTER_WAIT_TIMEOUT:-30}"
case "$wait_timeout" in
'' | *[!0-9]*)
	echo "entrypoint: MILTER_WAIT_TIMEOUT must be a whole number of seconds, got '$wait_timeout'" >&2
	exit 1
	;;
esac
export POSTFIX_STARTSECS=$((wait_timeout + 10))

exec /usr/bin/supervisord -c /etc/supervisor/supervisord.conf
