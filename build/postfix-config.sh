#!/bin/sh
# Generate the outbound-relay Postfix configuration.
#
# Run as root from entrypoint.sh on every container start, BEFORE supervisord
# starts the Postfix wrapper. Like the rest of SelfPost's runtime state it is
# re-derived from the environment on each start rather than persisted, so a
# fresh, restarted or restored container always ends up with the same config
# (the only panel-edited Postfix file, sender_login_maps, lives under /data and
# is generated separately by the panel).
#
# main.cf is written with `postconf -e`, the master.cf submission services with
# `postconf -M`/`-P`. No user input is interpolated: every value here comes from
# a fixed literal or a container environment variable.
set -eu

# Sets one -o option of the smtp/inet (port 25) service by patching its stanza.
#
# A master.cf -o value must not contain whitespace: master splits the line on
# it, so "-o name=check_recipient_access texthash:/x,reject" reaches smtpd as
# the option "name=check_recipient_access" plus a stray argument, and smtpd dies
# at start with "fatal: unexpected command-line argument" - port 25 then
# answers nobody. (postconf -P refuses such values for the same reason.) Postfix
# splits a restriction list on commas as well as on whitespace, so every value
# passed here is written with commas only; a value with whitespace is a bug in
# this script and stops the container.
#
# Postfix 3.10.13, man/man5/master.5:215-226, "-o name=value (short form)":
# "The "short form" does not support whitespace around the "=" or in
# parameter values" (the long form "-o { name = value }" would keep it; this
# script does not write it). src/postconf/postconf_edit.c:415,
# pcf_edit_master(): "-Pe does not accept whitespace in parameter value".
# src/smtpd/smtpd_check.c:664, smtpd_check_parse(), and src/milter/milter.c:646,
# milter_new(), split their lists on CHARS_COMMA_SP: commas or whitespace.
#
# master.5:41-42, SYNTAX: a line that starts with whitespace continues a
# logical line, so the "-o" line awk inserts below the header belongs to the
# service. A later postconf -F/-P re-parses a matched entry with its
# continuation lines (postconf_edit.c:134, pcf_gobble_cf_line()) and prints
# it whole again, keeping these options.
master_cf_set_smtp_inet_o() {
	param="$1"
	value="$2"
	case "$value" in
		*[[:space:]]*)
			echo "FATAL: master.cf -o ${param} would contain whitespace: ${value}" >&2
			exit 1
			;;
	esac
	awk -v p="$param" -v v="$value" '
		/^smtp[ \t]+inet[ \t]/ && !done {
			print
			print "    -o " p "=" v
			done = 1
			in_smtp_inet = 1
			next
		}
		in_smtp_inet && /^[ \t]+-o / {
			line = $0
			sub(/^[ \t]+-o /, "", line)
			eq = index(line, "=")
			on = substr(line, 1, eq - 1)
			if (on == p) {
				next
			}
		}
		in_smtp_inet && /^[^ \t#-]/ {
			in_smtp_inet = 0
		}
		{ print }
	' /etc/postfix/master.cf > /etc/postfix/master.cf.new
	mv /etc/postfix/master.cf.new /etc/postfix/master.cf
}

# --- environment knobs -------------------------------------------------------
# Server hostname: used as HELO name AND, crucially, as the Cyrus SASL realm the
# sasldb2 accounts are looked up under. The panel creates accounts under realm
# $SELFPOST_HOSTNAME (SASL_REALM), so myhostname MUST match or authentication
# fails. Fall back to the container hostname only outside a real deployment.
# Postfix 3.10.13, src/xsasl/xsasl_cyrus_server.c:339, xsasl_cyrus_server_create():
# sasl_server_new() gets var_myhostname as serverFQDN; Cyrus SASL 2.1.28,
# plugins/sasldb.c, sasldb_auxprop_lookup(): with no user realm the account is
# looked up under serverFQDN.
HOSTNAME_VALUE="${SELFPOST_HOSTNAME:-$(hostname -f 2>/dev/null || hostname)}"

# TLS material supplied by the reverse-proxy through a read-only bind mount. The
# relay requires TLS on 465; if these files are absent the master still starts
# but TLS handshakes on 465 fail until they appear. Postfix 3.10.13,
# src/smtpd/smtpd.c: pre_jail_init() only warns when the TLS context cannot be
# made, and smtpd_proto() then drops each wrapper-mode connection (:5846,
# "Wrapper-mode request dropped ... TLS context initialization failed"); every
# new smtpd process loads the files again in pre_jail_init().
#
# Postfix's TLS_README advises a private key owned by root and readable by no
# other user; Postfix does not enforce it (smtpd loads the key as root in
# pre_jail_init(), Postfix 3.10.13 src/smtpd/smtpd.c), but the mount cannot
# give that mode - it is :ro and owned by the host's user, often readable by
# group 10997 (the panel's alone) for the panel's backup. So the pair is copied into a directory of
# Postfix's own, the key root:root 0600, before postconf and `postfix check`
# (assumptions D-arch:154).
TLS_CERT_SRC="${TLS_CERT_FILE:-/etc/postfix/tls/fullchain.pem}"
TLS_KEY_SRC="${TLS_KEY_FILE:-/etc/postfix/tls/privkey.pem}"
TLS_INTERNAL_DIR=/etc/postfix/tls-internal
TLS_CERT="$TLS_CERT_SRC"
TLS_KEY="$TLS_KEY_SRC"
if [ -f "$TLS_CERT_SRC" ] && [ -f "$TLS_KEY_SRC" ]; then
	mkdir -p "$TLS_INTERNAL_DIR"
	cp -f "$TLS_CERT_SRC" "$TLS_INTERNAL_DIR/fullchain.pem"
	cp -f "$TLS_KEY_SRC" "$TLS_INTERNAL_DIR/privkey.pem"
	chown root:root "$TLS_INTERNAL_DIR/fullchain.pem" "$TLS_INTERNAL_DIR/privkey.pem"
	chmod 0644 "$TLS_INTERNAL_DIR/fullchain.pem"
	chmod 0600 "$TLS_INTERNAL_DIR/privkey.pem"
	TLS_CERT="$TLS_INTERNAL_DIR/fullchain.pem"
	TLS_KEY="$TLS_INTERNAL_DIR/privkey.pem"
fi

# Level-1 rate limit (native Postfix anvil). Conservative defaults, sensible
# during IP warm-up.
RATE_MSGS="${RATE_LIMIT_MESSAGES_PER_IP:-100}"
RATE_WINDOW="${RATE_LIMIT_WINDOW_SECONDS:-3600}"

# The settings below reach main.cf and master.cf as numbers or time values.
# A value that is neither, or one under the parameter's minimum, passes
# `postfix check` and kills the daemon that reads it - every smtpd at its
# first connection, or anvil and with it the rate limit (audit 2026-10-12,
# L31; review of stage 5) - so the start stops here, saying which and why.
# Counts and sizes are whole numbers from min up to 2147483647, an int's top
# (Postfix 3.10.13 reads the counts with get_mail_conf_int*, src/global/
# mail_conf_int.c, check_mail_conf_int(): an int, held to the parameter's own minimum;
# message_size_limit is a long, src/global/mail_params.h:2014, and is held to
# the int's top here all the same). A time value
# is a whole number from 1 with an optional unit, s m h d or w, at most
# 2147483647 seconds
# (proto/postconf.proto:266-268: "Specify a non-zero time value (an integral value
# plus an optional one-letter suffix ...). Time units: s (seconds), m
# (minutes), h (hours), d (days), w (weeks)"; one unit - "1h30m" is read as
# 1h, src/global/conv_time.c - so it is refused here).
need_number() {
	case "$2" in
	'' | *[!0-9]*)
		echo "postfix-config: $1 must be a whole number, got '$2'" >&2
		exit 1
		;;
	esac
	if [ "${#2}" -gt 10 ] || [ "$2" -lt "$3" ] || [ "$2" -gt 2147483647 ]; then
		echo "postfix-config: $1 must be from $3 to 2147483647, got '$2'" >&2
		exit 1
	fi
}
need_time() {
	if ! printf '%s' "$2" | grep -Eq '^[1-9][0-9]{0,8}[smhdw]?$'; then
		echo "postfix-config: $1 must be a time value - a whole number from 1 with an optional s, m, h, d or w - got '$2'" >&2
		exit 1
	fi
	# conv_time multiplies by the unit and refuses what passes an int.
	n=${2%[smhdw]}
	case "$2" in
	*w) n=$((n * 604800)) ;;
	*d) n=$((n * 86400)) ;;
	*h) n=$((n * 3600)) ;;
	*m) n=$((n * 60)) ;;
	esac
	if [ "$n" -gt 2147483647 ]; then
		echo "postfix-config: $1 is more than 2147483647 seconds, got '$2'" >&2
		exit 1
	fi
}
# A rate of 0 is no limit, a size of 0 none (proto/postconf.proto,
# smtpd_client_message_rate_limit, message_size_limit); the window is
# anvil_rate_time_unit, at least 1 second (src/anvil/anvil.c, main(): its
# time table's minimum 1).
need_number RATE_LIMIT_MESSAGES_PER_IP "$RATE_MSGS" 0
need_number RATE_LIMIT_WINDOW_SECONDS "$RATE_WINDOW" 1
need_number INBOUND_RATE_LIMIT_MESSAGES_PER_IP "${INBOUND_RATE_LIMIT_MESSAGES_PER_IP:-20}" 0
need_number INBOUND_MESSAGE_SIZE_LIMIT "${INBOUND_MESSAGE_SIZE_LIMIT:-26214400}" 0
need_number DMARC_RATE_LIMIT_MESSAGES_PER_IP "${DMARC_RATE_LIMIT_MESSAGES_PER_IP:-20}" 0
need_number DMARC_MESSAGE_SIZE_LIMIT "${DMARC_MESSAGE_SIZE_LIMIT:-5242880}" 0
need_time MILTER_CONNECT_TIMEOUT "${MILTER_CONNECT_TIMEOUT:-15s}"
need_time MILTER_COMMAND_TIMEOUT "${MILTER_COMMAND_TIMEOUT:-15s}"
need_time MILTER_CONTENT_TIMEOUT "${MILTER_CONTENT_TIMEOUT:-30s}"

# Milter sockets, all the panel's: its journal-milter (monitoring,
# fail-open), the sender check and the DKIM signer (both strict). Fixed
# container paths, matched by postfix-wrapper.sh.
JOURNAL_SOCK="${JOURNAL_MILTER_SOCKET:-/run/selfpost/journal.sock}"
# The panel's sender check (the From: line against the login, strict).
SENDER_SOCK="${SENDER_CHECK_SOCKET:-/run/selfpost/sender.sock}"
# The panel's DKIM signer (strict): applications' mail, signed with the key
# of the login's domain, and the panel's own, with the server's name's
# (cmd/panel/signer.go). Matched by postfix-wrapper.sh.
SIGN_SOCK="${DKIM_SIGN_SOCKET:-/run/selfpost/sign.sock}"
SIGN_LOCAL_SOCK="${DKIM_SIGN_LOCAL_SOCKET:-/run/selfpost/sign-local.sock}"

# Persistent panel-managed sender map; texthash needs no postmap, so the
# unprivileged panel can rewrite it and just ask for a reload. Postfix 3.10.13,
# html/DATABASE_README.html:458-464, "texthash": no postmap(1), and the file is read
# once by each process - a reload makes the new processes read it again.
SENDER_LOGIN_MAPS="${POSTFIX_SENDER_LOGIN_MAPS:-/data/postfix/sender_login_maps}"
# Transit mail queue under /data so deferred/active messages survive container
# recreate (architecture.md § Persistence). Distinct from sender_login_maps.
QUEUE_DIR="${POSTFIX_QUEUE_DIR:-/data/postfix/queue}"
SASLDB_PATH="${SASL_DB_PATH:-/data/sasl/sasldb2}"

# Optional submission service on 587 (off by default, enabled only when a client
# library needs STARTTLS on 587 instead of implicit TLS on 465).
SUBMISSION_ENABLE="${SUBMISSION_ENABLE:-false}"

# Optional inbound relay (backup-MX / forwarder). Off by default: port 25 does
# not accept mail, Postfix inbound maps are not referenced, and the panel UI is
# absent. When true, smtp inet on 25 accepts only relay_domains + known
# recipients (docs/plans/inbound-relay.md).
INBOUND_RELAY_ENABLE="${INBOUND_RELAY_ENABLE:-false}"
INBOUND_ANTISPAM_MILTER="${INBOUND_ANTISPAM_MILTER:-}"
INBOUND_ANTISPAM_MILTER_ACTION="${INBOUND_ANTISPAM_MILTER_ACTION:-accept}"
# The panel's inbound journal milter (monitoring, fail-open), on port 25 only
# and only while the inbound relay is on. Matched by postfix-wrapper.sh.
INBOUND_JOURNAL_SOCK="${INBOUND_JOURNAL_MILTER_SOCKET:-/run/selfpost/inbound-journal.sock}"
INBOUND_RATE_LIMIT_MESSAGES_PER_IP="${INBOUND_RATE_LIMIT_MESSAGES_PER_IP:-20}"
INBOUND_MESSAGE_SIZE_LIMIT="${INBOUND_MESSAGE_SIZE_LIMIT:-26214400}"
RELAY_DOMAINS_MAP="${POSTFIX_RELAY_DOMAINS:-/data/postfix/relay_domains}"
TRANSPORT_MAP="${POSTFIX_TRANSPORT_MAPS:-/data/postfix/transport}"
RELAY_RECIPIENTS_MAP="${POSTFIX_RELAY_RECIPIENTS:-/data/postfix/relay_recipients}"
TLS_POLICY_MAP="${POSTFIX_TLS_POLICY_MAPS:-/data/postfix/tls_policy}"

# Optional DMARC aggregate ingest (plans/dmarc-reports.md). Off by default:
# port 25 does not accept report mail and the panel ingest maps stay empty.
DMARC_REPORTS_ENABLE="${DMARC_REPORTS_ENABLE:-false}"
DMARC_RATE_LIMIT_MESSAGES_PER_IP="${DMARC_RATE_LIMIT_MESSAGES_PER_IP:-20}"
DMARC_MESSAGE_SIZE_LIMIT="${DMARC_MESSAGE_SIZE_LIMIT:-5242880}"
DMARC_RECIPIENTS_MAP="${POSTFIX_DMARC_RECIPIENTS:-/data/postfix/dmarc_recipients}"
DMARC_TRANSPORT_MAP="${POSTFIX_DMARC_TRANSPORT:-/data/postfix/dmarc_transport}"
DMARC_RELAY_DOMAINS_MAP="${POSTFIX_DMARC_RELAY_DOMAINS:-/data/postfix/dmarc_relay_domains}"

# Delivery log, written by postlogd and read by the panel's log-tailer. It lives
# under the persistent /data (not the ephemeral /var/log) so the delivery lines
# for messages still marked "queued" survive a container recreate — without
# them those rows could never be resolved (architecture.md § Log tailer). The
# default must match cmd/panel/main.go's MAIL_LOG; entrypoint.sh creates the
# directory and the file with the ownership postlogd writes and the panel reads.
MAIL_LOG_PATH="${MAIL_LOG:-/data/log/mail.log}"

# /etc/postfix outlives a restart of the container, so a cleanup limit set for
# port 25 before is taken back here and set again below when it still applies.
postconf -PX "cleanup/unix/message_size_limit" 2>/dev/null || true

# --- main.cf -----------------------------------------------------------------
# maillog_file lives under the persistent /data bind mount (architecture.md §
# Log tailer). Postfix's default maillog_file_prefixes are only /var and
# /dev/stdout — without /data, `postfix check` rejects the path.
# inet_protocols=all in a bridge network without IPv6: Postfix 3.10.13,
# src/util/inet_proto.c:200, inet_proto_init(), keeps IPv6 when socket(PF_INET6)
# succeeds, and src/smtp/smtp_connect.c:1102-1142, smtp_connect_inet(), goes on
# to the next address when a connection fails - an AAAA that cannot be reached
# costs one failed attempt, then IPv4. Postfix
# 3.10.13, src/global/mail_params.h:4406, DEF_MAILLOG_FILE_PFXS "/var, /dev/stdout";
# src/global/maillog_client.c:232, maillog_client_init(): fatal "does not match any
# prefix", in every Postfix program that logs, postfix(1) itself included.
#
# maillog_file_permissions=0640: the log is the panel's to read, through
# group selfpost (the setgid /data/log, build/entrypoint.sh). entrypoint.sh
# and logrotate's "create" make it 0640 themselves; this is for a file that
# Postfix makes - one removed while the container runs - which would
# otherwise be 0600 and unreadable to the panel. (Postfix 3.10.13,
# src/global/mail_params.h:4418, DEF_MAILLOG_FILE_PERMS "0600";
# src/global/mail_params.c:997, set_logwriter_create_perms(); src/util/
# logwriter.c:143-148: "644", "640" or "600", leading zeros ignored, anything
# else a warning and 0600; :99, logwriter_open_or_die() creates with it;
# Postfix 3.9 and later, proto/postconf.proto:18500-18509.)
postconf -e \
	"myhostname=${HOSTNAME_VALUE}" \
	"maillog_file=${MAIL_LOG_PATH}" \
	"maillog_file_prefixes=/var,/dev/stdout,/data" \
	"maillog_file_permissions=0640" \
	"mydestination=" \
	"relayhost=" \
	"inet_interfaces=all" \
	"inet_protocols=all"

# postlogd is mandatory whenever maillog_file is set (Postfix 3.10.13,
# html/MAILLOG_README.html:42, "Configuring logging to file"; conf/postfix-script:333,
# "check-fatal": "missing 'postlog' service in master.cf"). Debian's
# stock master.cf usually has it; pin it explicitly so a stripped/upgraded
# image cannot lose the service.
postconf -M "postlog/unix-dgram=postlog unix-dgram n - n - 1 postlogd"

# This is an outbound relay: no local delivery, no per-user aliases. Empty
# these so a misfiled recipient never gets delivered locally. An empty
# mydestination is not enough: an address literal of an interface the
# container listens on - x@[127.0.0.1] - is local too (Postfix 3.10.13,
# src/global/resolve_local.c, resolve_local(), own_inet_addr()), so local(8)
# itself is replaced by a refusal, as Postfix's null-client example does
# (README_FILES/STANDARD_CONFIGURATION_README:209, "local_transport = error:local
# mail delivery is disabled"; assumptions C-P8).
postconf -e \
	"local_recipient_maps=" \
	"alias_maps=" \
	"alias_database=" \
	"local_transport=error:local mail delivery is disabled"

# Outbound delivery: straight to the recipient MX, opportunistic TLS.
postconf -e \
	"smtp_tls_security_level=may" \
	"smtp_tls_note_starttls_offer=yes"

# TLS server material shared by every inbound service. auth_only guarantees
# credentials are never accepted before TLS is up on any port. Postfix 3.10.13,
# src/smtpd/smtpd_sasl_proto.c:188, smtpd_sasl_auth_cmd(): "504 5.5.4 Encryption
# required" while auth_only is set and TLS is not up; src/smtpd/smtpd.c:6546-6547,
# pre_jail_init(): wrappermode implies enforce, and enforce implies auth_only.
postconf -e \
	"smtpd_tls_cert_file=${TLS_CERT}" \
	"smtpd_tls_key_file=${TLS_KEY}" \
	"smtpd_tls_security_level=may" \
	"smtpd_tls_auth_only=yes" \
	"smtpd_tls_loglevel=1"

# TLS 1.2 at least, opportunistic or required - and the client's too where an
# inbound domain requires TLS to its upstream (smtp_tls_mandatory_protocols,
# the security review of stage 5) (audit 2026-10-12, L31; Postfix
# 3.10.13, proto/postconf.proto:13240-13250, smtpd_tls_protocols: since 3.6 the
# default is ">=TLSv1", and a lower bound is written ">=version"). A sender that
# offers less on port 25 sends in the clear, as one without TLS does. VRFY
# answers no question about who is accepted on port 25 (relay_recipient_maps),
# and the banner names no distribution (Debian's adds "(Debian)": postfix
# 3.10.13-0+deb13u1, debian/main.cf.in:15 "(@DEB_VENDOR@)", filled in by
# debian/rules:118; the upstream default is "$myhostname ESMTP $mail_name").
# ETRN is refused to everyone and not offered: Postfix's default lets any
# client flush the queue toward an inbound domain's upstream
# (fast_flush_domains = $relay_domains), and nothing here needs it (security
# audit 2026-10-17, S6; Postfix 3.10.13, src/smtpd/smtpd.c, etrn_cmd() asks
# smtpd_check_etrn(), whose "reject" answers reject_code, 554).
postconf -e \
	"smtpd_tls_protocols=>=TLSv1.2" \
	"smtpd_tls_mandatory_protocols=>=TLSv1.2" \
	"smtp_tls_mandatory_protocols=>=TLSv1.2" \
	"disable_vrfy_command=yes" \
	"smtpd_etrn_restrictions=reject" \
	"smtpd_discard_ehlo_keywords=etrn" \
	'smtpd_banner=$myhostname ESMTP'

# SASL: Cyrus with the local sasldb2 the panel maintains. The realm is left
# implicit (smtpd_sasl_local_domain empty) so the authenticated name Postfix
# uses for sender_login_maps is the BARE login the panel writes into the map;
# the sasldb2 lookup still resolves because Postfix hands Cyrus $myhostname as
# the server realm, which equals the realm the accounts were created under.
# (Postfix 3.10.13, src/smtpd/smtpd_sasl_glue.c:249, smtpd_sasl_activate(): an empty
# smtpd_sasl_local_domain is a NULL user realm; see HOSTNAME_VALUE above.)
postconf -e \
	"smtpd_sasl_auth_enable=yes" \
	"smtpd_sasl_type=cyrus" \
	"smtpd_sasl_path=smtpd" \
	"smtpd_sasl_local_domain=" \
	"smtpd_sasl_security_options=noanonymous" \
	"smtpd_sasl_tls_security_options=noanonymous" \
	"broken_sasl_auth_clients=yes"

# Sender binding (the critical anti-spoofing control). smtpd looks the sender
# up in this map with mail_addr_find(), the order being smtpd's and not
# texthash's: the full address, the address without its extension, then the
# "@domain" wildcard - so both address modes work from the same map (Postfix
# 3.10.13, src/smtpd/smtpd_check.c:3933, reject_auth_sender_login_mismatch();
# src/global/mail_addr_find.c, mail_addr_find_opt() with MA_FIND_DEFAULT).
#
# recipient_delimiter is said here and not left to the distribution's main.cf:
# it is what lets an application listed as invoices@ send as invoices+march@,
# and the panel's sender check makes the same second try for the From: line
# (extensionDelimiter in internal/milter/sender.go) - the two must agree.
# (mail_addr_find_opt() strips the extension at var_rcpt_delim,
# strip_addr_internal(), for its second try.)
postconf -e \
	"smtpd_sender_login_maps=texthash:${SENDER_LOGIN_MAPS}" \
	"recipient_delimiter=+" \
	"queue_directory=${QUEUE_DIR}"

# Restrictions: authenticated clients only, no relay to foreign destinations,
# and every authenticated sender address must be owned by its login. NO
# permit_mynetworks anywhere — authorisation is by credentials, never by
# network. This is what makes an open relay impossible. (Postfix 3.10.13,
# src/smtpd/smtpd_check.c:5076, smtpd_check_rcpt(), :5160-5173: the relay and
# recipient lists are evaluated in turn and the first REJECT ends them.)
postconf -e \
	"smtpd_helo_required=yes" \
	"smtpd_relay_restrictions=permit_sasl_authenticated, reject_unauth_destination" \
	"smtpd_recipient_restrictions=permit_sasl_authenticated, reject_unauth_destination" \
	"smtpd_sender_restrictions=reject_sender_login_mismatch, permit"

# Level-1 rate limit by client IP. Backstop that keeps working even if the
# journal-milter (level 2) is down. Postfix 3.10.13, src/smtpd/smtpd.c:2566,
# mail_cmd(), :2624: "450 4.7.1 Error: too much mail from", except for clients
# in smtpd_client_event_limit_exceptions (:2619, hogger_list; default
# $mynetworks, src/smtpd/smtpd.c:779).
# anvil_rate_time_unit is one global window for every anvil limit (postconf(5)).
# "Client IP" is the address the container sees: an IPv4 client's own, but
# for a client over IPv6 the compose network's gateway, through Docker's
# proxy - measured on a live server, 2026-10-15, Docker 29.6.1 (assumptions
# C-K5; guide, "Rate limiting - level 1").
#
# mynetworks is said here, loopback only, and not left to the package: it is
# what the exceptions above and smtpd_forbid_bare_newline_exclusions (below)
# spare. The value is the one Debian's debconf default gives (postfix
# 3.10.13-0+deb13u1, debian/templates:124-126, "postfix/mynetworks", written
# by debian/postfix.postinst:249-262 on a new main.cf, where :180,
# fset_all_changed, marks every debconf value to be written; main.cf.in:28 has 127.0.0.0/8
# alone), so on this image it changes nothing.
postconf -e \
	"mynetworks=127.0.0.0/8 [::ffff:127.0.0.0]/104 [::1]/128" \
	"smtpd_client_message_rate_limit=${RATE_MSGS}" \
	"anvil_rate_time_unit=${RATE_WINDOW}s"

# Milter chain. The sender check comes first and is strict
# (default_action=tempfail): it holds the From: line against the login, the
# address a receiver holds the signature to, so a message it could not judge
# must not be signed. While the panel does not answer on that socket, mail is
# deferred (docs/security.md).
#
# The panel's DKIM signer follows, strict too: unreachable, it defers rather
# than send unsigned (docs/plans/panel-dkim-signing.md). It signs with the
# key of the login's domain. The journal-milter is monitoring only and is
# fail-open (default_action=accept): its failure must never block the relay.
# Per-milter settings use Postfix 3.0+ brace syntax.
#
# non_smtpd_milters is the chain for mail submitted locally with sendmail(1) -
# the panel's own notifications, sent as postmaster@ the hostname. The
# signer's own socket for it signs them with the key of the server's own
# name; the journal-milter is not on this chain, since they are not an
# application's mail. Bounces Postfix
# generates itself do not pass it (internal_mail_filter_classes is empty).
#
# Postfix 3.10.13: html/MILTER_README.html:584, "Different settings for different
# Milter applications" (the braces, Postfix 3.0 and later) and :449, "Signing
# internally-generated bounce messages"; src/milter/milter.c:399-564,
# milter_*_event() and milter_message(): milters are asked in list order and
# the first non-empty reply ends the round; src/global/mail_params.h:3599,
# DEF_INT_FILT_CLASSES INT_FILT_CLASS_NONE.
postconf -e \
	"milter_protocol=6" \
	"milter_default_action=tempfail" \
	"smtpd_milters={ unix:${SENDER_SOCK}, default_action=tempfail }, { unix:${SIGN_SOCK}, default_action=tempfail }, { unix:${JOURNAL_SOCK}, default_action=accept }" \
	"non_smtpd_milters={ unix:${SIGN_LOCAL_SOCK}, default_action=tempfail }"

# A signed message is delivered as it was signed: Postfix's SMTP client would
# otherwise rewrite an 8-bit message as quoted-printable for a server that
# does not announce 8BITMIME, and every body hash over it would fail.
# Postfix 3.10.13, src/global/mail_params.h:3088, DEF_DISABLE_MIME_OCONV 0
# (the conversion is on by default); proto/postconf.proto:7992,
# disable_mime_output_conversion: "Disable the conversion of 8BITMIME format
# to 7BIT format". Not set until 2.8.0 - OpenDKIM's signatures, before the
# panel's, were exposed to it as well.
postconf -e "disable_mime_output_conversion=yes"

# Long, non-repeating queue ids (owner, 2026-10-16). The panel finds a
# message's rows, its stored lines and the inbound journal's upstream result
# by the queue id of mail.log; a short id is the microsecond of arrival and
# the queue file's inode in hex and repeats once the inode comes round, so a
# later message could meet an earlier one's rows. A long id adds the time in
# seconds and does not repeat. Postfix 3.10.13, src/global/mail_params.h:1010,
# DEF_LONG_QUEUE_IDS 0; src/global/mail_queue.h, MQID_SH_* and MQID_LG_*;
# proto/postconf.proto, enable_long_queue_ids: "Enable long, non-repeating,
# queue IDs", existing queue files keep their names, postqueue -p gets a
# wider Queue ID column. Every pattern of the panel takes both shapes
# (internal/logtail, internal/postfix/queue.go).
postconf -e "enable_long_queue_ids=yes"

# What Postfix tells a milter with MAIL FROM: its defaults plus the client's
# address. The panel's milters judge every message by that address (an
# application's client-address list) and record it (the inbound journal), and
# the address given once at connect is not there for a connection's later
# messages - see MailFrom in internal/milter/milter.go. The defaults: Postfix
# 3.10.13, src/global/mail_params.h:3532-3534, DEF_MILT_MAIL_MACROS.
postconf -e \
	"milter_mail_macros=i {auth_type} {auth_authen} {auth_author} {mail_addr} {mail_host} {mail_mailer} {client_addr}"

# Bounded milter timeouts: a *hung* milter (socket accepts but never replies)
# must fail open just like a crash, not stall mail acceptance until the Postfix
# defaults (300s content) elapse. With default_action per milter, a
# journal-milter hang then resolves to accept and a signer hang to tempfail,
# but within seconds rather than minutes. Values are well above any healthy
# response time (signing/DB insert are sub-second), so they never fire in normal
# operation. The defaults: Postfix 3.10.13, src/global/mail_params.h:3559-3567,
# DEF_MILT_CONN_TIME "30s", DEF_MILT_CMD_TIME "30s", DEF_MILT_MSG_TIME "300s".
postconf -e \
	"milter_connect_timeout=${MILTER_CONNECT_TIMEOUT:-15s}" \
	"milter_command_timeout=${MILTER_COMMAND_TIMEOUT:-15s}" \
	"milter_content_timeout=${MILTER_CONTENT_TIMEOUT:-30s}"

# --- master.cf: inbound submission services ----------------------------------
# smtps (465, implicit/wrapper TLS) — the primary, always-on submission
# service. chroot=n so smtpd can read the sasldb2 and sender map under /data
# and the Cyrus config outside any chroot.
# The names smtps, submission and smtp need no /etc/services: Postfix 3.10.13,
# src/global/mail_params.h:4450-4451, DEF_KNOWN_TCP_PORTS "lmtp=24, smtp=25,
# smtps=submissions=465, submission=587" (src/util/find_inet.c:94-99,
# find_inet_port() via filter_known_tcp_port()).
#
# smtpd_hide_client_session=yes on both submission services (owner's
# decision, 2026-10-15): the Received: header Postfix adds names neither the
# application's HELO name, reverse name and address, nor its TLS session,
# SASL login or the protocol - only "Received: by <myhostname> (Postfix) id
# <queue id>", "for <rcpt>" when there is one recipient, and the date
# (Postfix 3.10.13, src/smtpd/smtpd.c:3495, the client part written only
# when the parameter is off; :3627-3645, the "by" line opening the header
# when it is on). proto/postconf.proto:19580-19594: "may be used for the
# port 587 and 465 MUA services"; port 25 must keep the default, RFC 5321's
# trace, and does (smtp/inet below sets nothing). The queue id stays in the
# header, so a complaint about a message still leads to its row in the
# panel's log and to smtpd's "client=" line in mail.log.
postconf -M "smtps/inet=smtps inet n - n - - smtpd"
postconf -P \
	"smtps/inet/smtpd_hide_client_session=yes" \
	"smtps/inet/smtpd_tls_wrappermode=yes" \
	"smtps/inet/smtpd_sasl_auth_enable=yes" \
	"smtps/inet/smtpd_client_restrictions=permit_sasl_authenticated,reject"

# submission (587, STARTTLS) — optional. Same SASL/milter/limits; the only
# difference is TLS is negotiated via STARTTLS, so require encryption before
# auth. Added only when SUBMISSION_ENABLE=true, otherwise removed so a restart
# after disabling it does not leave the port listening. (Postfix 3.10.13,
# src/smtpd/smtpd.c:6525-6526, pre_jail_init(): smtpd_tls_security_level=encrypt
# sets enforce_tls, which implies smtpd_tls_auth_only (:6547);
# src/postconf/postconf_edit.c:319, pcf_edit_master(): -MX drops the matched
# entry, continuation lines included.)
if [ "${SUBMISSION_ENABLE}" = "true" ]; then
	postconf -M "submission/inet=submission inet n - n - - smtpd"
	postconf -P \
		"submission/inet/smtpd_hide_client_session=yes" \
		"submission/inet/smtpd_tls_security_level=encrypt" \
		"submission/inet/smtpd_sasl_auth_enable=yes" \
		"submission/inet/smtpd_client_restrictions=permit_sasl_authenticated,reject"
else
	postconf -MX "submission/inet" 2>/dev/null || true
fi

# --- inbound smtpd on port 25 (optional inbound relay and/or DMARC ingest) -----
# Debian's stock master.cf enables smtp/inet (postfix 3.10.13-0+deb13u1,
# debian/patches/40_chroot_by_default.diff:16: "smtp inet n - y - - smtpd",
# uncommented; conf/master.cf:12). When both flags are off, remove
# that listener so port 25 is not an inbound smtpd (outbound delivery uses
# smtp/unix). When either is on: no SASL, no signing; relay accepts only
# configured domains/recipients; DMARC accepts only allow-listed report addresses.
if [ "${INBOUND_RELAY_ENABLE}" = "true" ] || [ "${DMARC_REPORTS_ENABLE}" = "true" ]; then
	RELAY_DOMAINS_SETTING=""
	TRANSPORT_SETTING=""
	RECIPIENT_RESTRICTIONS="reject"
	if [ "${INBOUND_RELAY_ENABLE}" = "true" ]; then
		# [ -e ] is false for a dangling link and the redirect would follow
		# it: the entrypoint's drop_links removes links at these paths, and
		# this refuses to create through one all the same (audit 2026-10-17,
		# I2).
		for f in "$RELAY_DOMAINS_MAP" "$TRANSPORT_MAP" "$RELAY_RECIPIENTS_MAP" "$TLS_POLICY_MAP"; do
			[ -e "$f" ] || [ -L "$f" ] || : > "$f"
		done
		RELAY_DOMAINS_SETTING="texthash:${RELAY_DOMAINS_MAP}"
		TRANSPORT_SETTING="texthash:${TRANSPORT_MAP}"
		RECIPIENT_RESTRICTIONS="reject_unauth_destination,reject_unlisted_recipient"
	fi
	if [ "${DMARC_REPORTS_ENABLE}" = "true" ]; then
		for f in "$DMARC_RECIPIENTS_MAP" "$DMARC_TRANSPORT_MAP" "$DMARC_RELAY_DOMAINS_MAP"; do
			[ -e "$f" ] || [ -L "$f" ] || : > "$f"
		done
		ln -sf /usr/local/bin/panel /usr/local/bin/dmarc-ingest
		# flags=, user= and argv= are arguments of pipe(8), not -o parameters:
		# postconf -P refuses them ("unmatched service_name/type") and leaves a
		# bare "pipe", which dies with "fatal: missing user= command-line
		# attribute" on every report. They go into the service line itself.
		# (Seen in 1.7.0 through 1.9.5, date not recorded, on Postfix 3.7.11.
		# Postfix 3.10.13: src/postconf/postconf_master.c:1029,
		# pcf_edit_master_param(), -P writes "-o name=value" and nothing else;
		# postconf_edit.c:617 warns "unmatched service_name/type" for a service
		# master.cf does not have; src/pipe/pipe.c:867, get_service_attr(),
		# :1018: "missing user= command-line attribute".)
		# ${recipient} is expanded by pipe(8), not by the shell: the command
		# learns which report address the message came to from its argument
		# (man/man8/pipe.8:289, ${recipient}: as many arguments as recipients).
		# One at a time (the 1 before "pipe"): reports are in no hurry, every run
		# writes to the database the panel writes to, and a hundred of them in
		# parallel - Postfix's default - is what a flood of reports would start.
		postconf -M 'dmarc-ingest/unix=dmarc-ingest unix - n n - 1 pipe flags=Rq user=panel argv=/usr/local/bin/dmarc-ingest ${recipient}'
		# One recipient per run, so ${recipient} is one argument.
		postconf -e "dmarc-ingest_destination_recipient_limit=1"
		# pipe(8) starts the command with export_environment only, so what the
		# command reads from the container's environment is named there.
		# export_environment is a list split at whitespace in which Postfix
		# expands "$": a path with either would reach the command as another
		# path, and it would quietly open a database of its own. (Postfix 3.10.13,
		# man/man8/pipe.8:417, export_environment; src/global/mail_params.h:2650,
		# DEF_EXPORT_ENVIRON "TZ MAIL_CONFIG LANG"; src/util/clean_env.c. The
		# parameter is main.cf's, so every Postfix process exports these too.)
		case "${SELFPOST_DATA_DIR:-}${SELFPOST_DB_PATH:-}" in
			*[[:space:]]* | *'$'*)
				echo "postfix-config: SELFPOST_DATA_DIR and SELFPOST_DB_PATH must not contain whitespace or \$ when DMARC_REPORTS_ENABLE=true" >&2
				exit 1
				;;
		esac
		DMARC_INGEST_ENV="DMARC_REPORTS_ENABLE=true"
		[ -z "${SELFPOST_DATA_DIR:-}" ] || DMARC_INGEST_ENV="${DMARC_INGEST_ENV} SELFPOST_DATA_DIR=${SELFPOST_DATA_DIR}"
		[ -z "${SELFPOST_DB_PATH:-}" ] || DMARC_INGEST_ENV="${DMARC_INGEST_ENV} SELFPOST_DB_PATH=${SELFPOST_DB_PATH}"
		postconf -e "export_environment=TZ MAIL_CONFIG LANG ${DMARC_INGEST_ENV}"
		if [ -n "$RELAY_DOMAINS_SETTING" ]; then
			RELAY_DOMAINS_SETTING="${RELAY_DOMAINS_SETTING} texthash:${DMARC_RELAY_DOMAINS_MAP}"
			TRANSPORT_SETTING="${TRANSPORT_SETTING} texthash:${DMARC_TRANSPORT_MAP}"
		else
			RELAY_DOMAINS_SETTING="texthash:${DMARC_RELAY_DOMAINS_MAP}"
			TRANSPORT_SETTING="texthash:${DMARC_TRANSPORT_MAP}"
		fi
		# Comma, not a space, between check_recipient_access and its table: see
		# master_cf_set_smtp_inet_o (master.5, "-o name=value (short form)": "use
		# commas instead of spaces if the parameter supports that").
		RECIPIENT_RESTRICTIONS="check_recipient_access,texthash:${DMARC_RECIPIENTS_MAP},${RECIPIENT_RESTRICTIONS}"
	else
		postconf -MX "dmarc-ingest/unix" 2>/dev/null || true
		postconf -X dmarc-ingest_destination_recipient_limit export_environment 2>/dev/null || true
	fi
	postconf -e \
		"relay_domains=${RELAY_DOMAINS_SETTING}" \
		"transport_maps=${TRANSPORT_SETTING}" \
		"smtpd_reject_unlisted_recipient=yes"
	if [ "${INBOUND_RELAY_ENABLE}" = "true" ]; then
		# With the relay on, Postfix refuses every recipient of a relay domain
		# that relay_recipient_maps does not list (smtpd_reject_unlisted_recipient
		# above) - and it does so after the restriction list, whatever
		# check_recipient_access answered. The DMARC report addresses live on a
		# relay domain too (the server's own name), so their table is a recipient
		# table as well; without it they are "User unknown in relay recipient
		# table" and no report is ever accepted while both features are on.
		# (Postfix 3.10.13, src/smtpd/smtpd_check.c, smtpd_check_rcpt(),
		# :5196-5200: after the lists, check_recipient_rcpt_maps() runs unless
		# the status is REJECT - an OK from check_recipient_access does not
		# skip it.)
		RELAY_RECIPIENT_SETTING="texthash:${RELAY_RECIPIENTS_MAP}"
		if [ "${DMARC_REPORTS_ENABLE}" = "true" ]; then
			RELAY_RECIPIENT_SETTING="${RELAY_RECIPIENT_SETTING} texthash:${DMARC_RECIPIENTS_MAP}"
		fi
		# The panel writes the same "[host]:port" into transport and
		# tls_policy: the policy lookup key is the verbatim next-hop, port and
		# brackets included (Postfix 3.10.13, README_FILES/TLS_README:914, "TLS
		# policy table" note: ":smtp", ":25" and no port are different keys).
		postconf -e \
			"relay_recipient_maps=${RELAY_RECIPIENT_SETTING}" \
			"smtp_tls_policy_maps=texthash:${TLS_POLICY_MAP}"
	else
		postconf -e \
			"relay_recipient_maps=" \
			"smtp_tls_policy_maps="
	fi

	# Milters on port 25. main.cf's chain (sender check and signer strict +
	# journal-milter) is for the SUBMISSION path only: the signer must not gate someone else's
	# inbound mail (a signing outage would defer it), and the journal-milter
	# would file inbound mail into the outbound send log under the *sender's*
	# domain — polluting Deliveries and letting a forged From: consume that
	# domain's level-2 rate-limit budget. So smtpd_milters is always overridden
	# here: the optional antispam milter, then - with the inbound relay on - the
	# panel's inbound journal milter, or nothing at all. (Postfix 3.10.13,
	# man/man5/master.5: "-o" overrides the main.cf parameter for this service;
	# src/smtpd/smtpd.c:3475 hands its own milter list to cleanup, milter_send(),
	# and src/cleanup/cleanup_milter.c:1997, cleanup_milter_receive(), uses it.)
	INBOUND_MILTERS=""
	if [ "${INBOUND_RELAY_ENABLE}" = "true" ] && [ -n "${INBOUND_ANTISPAM_MILTER}" ]; then
		# A glob's "*" matches anything, so the shape alone would pass
		# "inet:a:1},{unix:/elsewhere": characters outside the set are refused
		# first, then the shape is checked.
		case "${INBOUND_ANTISPAM_MILTER}" in
			*[!A-Za-z0-9._:/-]*)
				echo "FATAL: INBOUND_ANTISPAM_MILTER must be inet:host:port or unix:/path, got: ${INBOUND_ANTISPAM_MILTER}" >&2
				exit 1
				;;
			inet:[A-Za-z0-9._-]*:[0-9]* | unix:/[A-Za-z0-9._/-]* ) ;;
			*)
				echo "FATAL: INBOUND_ANTISPAM_MILTER must be inet:host:port or unix:/path, got: ${INBOUND_ANTISPAM_MILTER}" >&2
				exit 1
				;;
		esac
		case "${INBOUND_ANTISPAM_MILTER_ACTION}" in
			accept|tempfail) ;;
			*)
				echo "FATAL: INBOUND_ANTISPAM_MILTER_ACTION must be accept or tempfail, got: ${INBOUND_ANTISPAM_MILTER_ACTION}" >&2
				exit 1
				;;
		esac
		INBOUND_MILTERS="{${INBOUND_ANTISPAM_MILTER},default_action=${INBOUND_ANTISPAM_MILTER_ACTION}}"
	fi
	if [ "${INBOUND_RELAY_ENABLE}" = "true" ]; then
		# Last in the chain: Postfix hands a message to its milters one after
		# another at end-of-message, so this one reads the verdict headers the
		# filter added and sees only what the filter let through. It watches and
		# never refuses, and must never hold up someone else's mail: accept when
		# it is down. A comma, not a space, between the two - see
		# master_cf_set_smtp_inet_o.
		# Postfix 3.10.13: src/milter/milter.c:564, milter_message(), sends the
		# message to each milter in list order; src/milter/milter8.c:960,
		# milter8_event(), applies a header a milter adds (SMFIR_ADDHEADER,
		# :1424) to the queue file at once, and the next milter8_message()
		# (:2544) reads that file with rec_get() (:2612), which follows the
		# added records.
		case "${INBOUND_JOURNAL_SOCK}" in
			*[!A-Za-z0-9._/-]*)
				echo "FATAL: INBOUND_JOURNAL_MILTER_SOCKET must be an absolute path, got: ${INBOUND_JOURNAL_SOCK}" >&2
				exit 1
				;;
			/[A-Za-z0-9._/-]*) ;;
			*)
				echo "FATAL: INBOUND_JOURNAL_MILTER_SOCKET must be an absolute path, got: ${INBOUND_JOURNAL_SOCK}" >&2
				exit 1
				;;
		esac
		INBOUND_MILTERS="${INBOUND_MILTERS:+${INBOUND_MILTERS},}{unix:${INBOUND_JOURNAL_SOCK},default_action=accept}"
	fi

	PORT25_RATE="${INBOUND_RATE_LIMIT_MESSAGES_PER_IP}"
	PORT25_SIZE="${INBOUND_MESSAGE_SIZE_LIMIT}"
	if [ "${DMARC_REPORTS_ENABLE}" = "true" ]; then
		PORT25_RATE="${DMARC_RATE_LIMIT_MESSAGES_PER_IP}"
		PORT25_SIZE="${DMARC_MESSAGE_SIZE_LIMIT}"
	fi
	if [ "${INBOUND_RELAY_ENABLE}" = "true" ] && [ "${DMARC_REPORTS_ENABLE}" = "true" ]; then
		PORT25_RATE="${INBOUND_RATE_LIMIT_MESSAGES_PER_IP}"
		if [ "${INBOUND_MESSAGE_SIZE_LIMIT}" -gt "${DMARC_MESSAGE_SIZE_LIMIT}" ]; then
			PORT25_SIZE="${INBOUND_MESSAGE_SIZE_LIMIT}"
		else
			PORT25_SIZE="${DMARC_MESSAGE_SIZE_LIMIT}"
		fi
	fi

	# syslog_name: this listener's lines read postfix/inbound/smtpd, the
	# submission ports' postfix/smtpd. The log tailer files a refusal on port 25
	# into the inbound journal and has to tell the two apart. (Postfix 3.10.13,
	# src/global/mail_task.c:56, mail_task(): the log tag is syslog_name "/"
	# program name. Only smtpd carries it: cleanup and the rest are other
	# services and log as postfix/.)
	postconf -M "smtp/inet=smtp inet n - n - - smtpd"
	# smtpd_forbid_bare_newline: the 2023 SMTP-smuggling defence. normalize
	# takes a bare <LF> as a line end but ends DATA only at the standard
	# <CR><LF>.<CR><LF>: a "." line that a bare <LF> precedes or ends is
	# dropped, not taken as the end of the message (Postfix 3.10.13,
	# src/smtpd/smtpd.c:1693-1694, bare_lf_mask_table, normalize =
	# BARE_LF_FLAG_WANT_STD_EOD; :3712-3716, the DATA loop; RELEASE_NOTES-3.9:
	# 166-167, "the recommended setting "smtpd_forbid_bare_newline
	# = normalize""). Since 3.9 normalize is also the default (src/smtpd/
	# smtpd.c:830, "Postfix >= 3.9: normalize"; src/global/mail_params.h:4313,
	# DEF_SMTPD_FORBID_BARE_LF "normalize"); the line stays so that port 25
	# does not depend on a default (it came in 2.6.1, while the image's
	# Postfix 3.7.11 defaulted to "no").
	# Its exclusions default to $mynetworks (mail_params.h:4316), loopback
	# only here (above), so no client is spared. The submission ports take
	# the default, normalize as well since this Postfix: an application whose
	# SMTP library ends DATA with a bare <LF>.<LF> is no longer read to its
	# end there (own-mta-builds, § 5: their smuggled transaction would still
	# be the authenticated session's, so they were left to the default).
	postconf -P \
		"smtp/inet/smtpd_forbid_bare_newline=normalize" \
		"smtp/inet/syslog_name=postfix/inbound" \
		"smtp/inet/smtpd_sasl_auth_enable=no" \
		"smtp/inet/smtpd_tls_auth_only=no" \
		"smtp/inet/smtpd_sender_login_maps=" \
		"smtp/inet/smtpd_sender_restrictions=" \
		"smtp/inet/smtpd_client_restrictions=" \
		"smtp/inet/smtpd_relay_restrictions=reject_unauth_destination" \
		"smtp/inet/smtpd_client_message_rate_limit=${PORT25_RATE}" \
		"smtp/inet/message_size_limit=${PORT25_SIZE}"
	master_cf_set_smtp_inet_o smtpd_recipient_restrictions "${RECIPIENT_RESTRICTIONS}"
	master_cf_set_smtp_inet_o smtpd_milters "${INBOUND_MILTERS}"

	# smtpd's -o limit is not the whole of it: cleanup writes the queue file
	# under its own message_size_limit, main.cf's, which is never set here and
	# so is 10240000 (Postfix 3.10.13, src/cleanup/cleanup_init.c:444,
	# cleanup_post_jail(), :455, set_file_limit(); src/global/mail_params.h:2013,
	# DEF_MESSAGE_LIMIT), and a larger message is refused 552 after DATA
	# (assumptions C-P31). cleanup gets port 25's limit when it is larger;
	# smtpd on 465 and 587 still holds its own, main.cf's (src/smtpd/smtpd.c:3721,
	# :3964, :4136, :4250, the DATA and BDAT size checks).
	if [ "${PORT25_SIZE}" -gt "$(postconf -h message_size_limit)" ]; then
		postconf -P "cleanup/unix/message_size_limit=${PORT25_SIZE}"
	fi
else
	postconf -MX "smtp/inet" 2>/dev/null || true
	postconf -MX "dmarc-ingest/unix" 2>/dev/null || true
	postconf -X dmarc-ingest_destination_recipient_limit export_environment 2>/dev/null || true
	postconf -e \
		"relay_domains=" \
		"transport_maps=" \
		"relay_recipient_maps=" \
		"smtp_tls_policy_maps="
fi

# Disable chroot for every service. Debian ships the smtp delivery agent and
# others chrooted to /var/spool/postfix, where they cannot read /etc/resolv.conf
# — so outbound MX lookups fail with "Host not found" and mail never leaves.
# Inside a container the chroot buys little (the container is the isolation
# boundary) and breaks DNS/TLS trust-store access, so turn it off uniformly. Our
# own smtps/submission services are already n; this covers the delivery agents
# and the rest. (postfix 3.10.13-0+deb13u1: debian/patches/40_chroot_by_default.diff
# sets chroot y. chroot(2) also needs CAP_SYS_CHROOT, which
# deploy/docker-compose.yml drops with cap_drop: ALL - capabilities(7).)
#
# Debian's chroot setup now runs on every "postfix check" and "postfix
# start-fg": conf/post-install:587 (debian/patches/
# debian-run-configure-instance-from-create-missing.patch) calls
# /usr/lib/postfix/configure-instance.sh at the end of create-missing, and
# that copies etc/localtime, etc/resolv.conf, etc/hosts and the like into
# $queue_directory/etc - here /data/postfix/queue/etc - whether a service is
# chrooted or not (debian/configure-instance.in:80-91, before its
# need_chroot test at :93). With nothing chrooted those copies are read by
# nobody, so the image turns the sync off with SYNC_CHROOT= in
# /etc/default/postfix (build/Dockerfile; configure-instance.in:22-24, :44,
# "[ -n "$SYNC_CHROOT" ] || exit 0"; debian/README.Debian:136-138).
postconf -F "*/*/chroot=n"

# --- Cyrus SASL app config for smtpd -----------------------------------------
# Tells the Cyrus library (invoked by smtpd via smtpd_sasl_path=smtpd) to verify
# passwords straight from the panel-maintained sasldb2. PLAIN/LOGIN
# only — both are safe because TLS is mandatory before auth on every port.
# The directory is said here, cyrus_sasl_config_path, and not left to the
# package's main.cf. Postfix 3.10.13, src/xsasl/xsasl_cyrus_server.c:227-232,
# xsasl_cyrus_server_init(): a non-empty cyrus_sasl_config_path goes to Cyrus
# with sasl_set_path(SASL_PATH_TYPE_CONFIG, ...), and :246,
# sasl_server_init(callbacks, path_info) with smtpd_sasl_path=smtpd
# (src/smtpd/smtpd_sasl_glue.c:188-189) names smtpd.conf there; the default
# is empty (src/global/mail_params.h:1690, DEF_CYRUS_CONF_PATH ""), and then
# Cyrus looks in its own path, /etc/sasl2:/etc/sasl:/usr/lib/<triplet>/sasl2:
# /usr/lib/sasl2 (cyrus-sasl2 2.1.28+dfsg1, debian/rules:96, --with-configdir;
# lib/common.c:210, sasl_set_path()), which this file is not in. Bookworm's
# package found it with a patch (07_sasl_config.diff, gone in 3.10.13); trixie's
# writes cyrus_sasl_config_path = /etc/postfix/sasl into a new main.cf
# (postfix 3.10.13-0+deb13u1, debian/main.cf.in:71) and into an upgraded one
# only when it is empty (debian/postfix.postinst:88-97). The keys: Cyrus SASL
# 2.1.28, doc/legacy/options.html (pwcheck_method, auxprop_plugin,
# sasldb_path, mech_list).
postconf -e "cyrus_sasl_config_path=/etc/postfix/sasl"
mkdir -p /etc/postfix/sasl
cat > /etc/postfix/sasl/smtpd.conf <<EOF
pwcheck_method: auxprop
auxprop_plugin: sasldb
sasldb_path: ${SASLDB_PATH}
mech_list: PLAIN LOGIN
EOF

# Validate the generated configuration; fail loudly if postconf produced
# anything Postfix rejects, before the wrapper tries to start it. (Postfix
# 3.10.13, conf/postfix-script:292, "check": "check-fatal || exit 1"; check-fatal
# also runs "post-install create-missing" (:314), which builds the queue tree
# and, in Debian's package, runs configure-instance.sh - see the chroot note
# above.)
set +e
check_out=$(postfix check 2>&1)
ec=$?
set -e
if [ "$ec" -ne 0 ]; then
	echo "postfix-config: postfix check failed exit $ec" >&2
	printf '%s\n' "$check_out" >&2
	exit "$ec"
fi
