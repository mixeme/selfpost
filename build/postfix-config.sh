#!/bin/sh
# Generate the outbound-relay Postfix configuration (spec 5, 5.1, 5.2).
#
# Run as root from entrypoint.sh on every container start, BEFORE supervisord
# starts the Postfix wrapper. Like the rest of SelfPost's runtime state it is
# re-derived from the environment on each start rather than persisted, so a
# fresh, restarted or restored container always ends up with the same config
# (the only panel-edited Postfix file, sender_login_maps, lives under /data and
# is generated separately by the panel — spec 5.1).
#
# main.cf is written with `postconf -e`, the master.cf submission services with
# `postconf -M`/`-P`. No user input is interpolated: every value here comes from
# a fixed literal or a container environment variable (spec 7.6.3).
set -eu

# postconf -P refuses master.cf -o values that contain whitespace; inbound port
# 25 needs restriction lists and milter specs that include spaces. Patch the
# smtp/inet service stanza directly instead.
master_cf_set_smtp_inet_o() {
	param="$1"
	value="$2"
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

# --- environment knobs (spec 8) ----------------------------------------------
# Server hostname: used as HELO name AND, crucially, as the Cyrus SASL realm the
# sasldb2 accounts are looked up under. The panel creates accounts under realm
# $SELFPOST_HOSTNAME (SASL_REALM), so myhostname MUST match or authentication
# fails. Fall back to the container hostname only outside a real deployment.
HOSTNAME_VALUE="${SELFPOST_HOSTNAME:-$(hostname -f 2>/dev/null || hostname)}"

# TLS material supplied by the reverse-proxy through a read-only bind mount
# (spec 5.2). The relay requires TLS on 465; if these files are absent the
# master still starts but TLS handshakes on 465 fail until they appear.
#
# Postfix insists the private key is root-owned and mode 0600. The bind mount
# is often :ro and owned by the host user (e2e TempDir / CI runner UID), so
# copy into a writable internal dir and normalise ownership before postconf
# and `postfix check`.
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

# Level-1 rate limit (native Postfix anvil, spec 5 p.5 / 7.4). Conservative
# defaults, sensible during IP warm-up (spec 10).
RATE_MSGS="${RATE_LIMIT_MESSAGES_PER_IP:-100}"
RATE_WINDOW="${RATE_LIMIT_WINDOW_SECONDS:-3600}"

# Milter sockets: OpenDKIM (signing, strict) and the panel journal-milter
# (monitoring, fail-open). Fixed container paths, matched by postfix-wrapper.sh.
OPENDKIM_SOCK="${OPENDKIM_SOCKET:-/run/opendkim/opendkim.sock}"
JOURNAL_SOCK="${JOURNAL_MILTER_SOCKET:-/run/selfpost/journal.sock}"

# Persistent panel-managed sender map (spec 5.1); texthash needs no postmap, so
# the unprivileged panel can rewrite it and just ask for a reload.
SENDER_LOGIN_MAPS="${POSTFIX_SENDER_LOGIN_MAPS:-/data/postfix/sender_login_maps}"
# Transit mail queue under /data so deferred/active messages survive container
# recreate (architecture.md § Persistence). Distinct from sender_login_maps.
QUEUE_DIR="${POSTFIX_QUEUE_DIR:-/data/postfix/queue}"
SASLDB_PATH="${SASL_DB_PATH:-/data/sasl/sasldb2}"

# Optional submission service on 587 (spec 5 p.1: off by default, enabled only
# when a client library needs STARTTLS on 587 instead of implicit TLS on 465).
SUBMISSION_ENABLE="${SUBMISSION_ENABLE:-false}"

# Optional inbound relay (backup-MX / forwarder). Off by default: port 25 does
# not accept mail, Postfix inbound maps are not referenced, and the panel UI is
# absent. When true, smtp inet on 25 accepts only relay_domains + known
# recipients (docs/plans/inbound-relay.md).
INBOUND_RELAY_ENABLE="${INBOUND_RELAY_ENABLE:-false}"
INBOUND_ANTISPAM_MILTER="${INBOUND_ANTISPAM_MILTER:-}"
INBOUND_ANTISPAM_MILTER_ACTION="${INBOUND_ANTISPAM_MILTER_ACTION:-accept}"
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

# --- main.cf -----------------------------------------------------------------
# maillog_file lives under the persistent /data bind mount (architecture.md §
# Log tailer). Postfix's default maillog_file_prefixes are only /var and
# /dev/stdout — without /data, `postfix check` rejects the path.
postconf -e \
	"myhostname=${HOSTNAME_VALUE}" \
	"maillog_file=${MAIL_LOG_PATH}" \
	"maillog_file_prefixes=/var,/dev/stdout,/data" \
	"mydestination=" \
	"relayhost=" \
	"inet_interfaces=all" \
	"inet_protocols=all"

# postlogd is mandatory whenever maillog_file is set (MAILLOG_README). Debian's
# stock master.cf usually has it; pin it explicitly so a stripped/upgraded
# image cannot lose the service.
postconf -M "postlog/unix-dgram=postlog unix-dgram n - n - 1 postlogd"

# This is an outbound relay: no local delivery, no per-user aliases. Empty
# these so a misfiled recipient never gets delivered locally.
postconf -e \
	"local_recipient_maps=" \
	"alias_maps=" \
	"alias_database="

# Outbound delivery: straight to the recipient MX, opportunistic TLS (spec 5 p.2).
postconf -e \
	"smtp_tls_security_level=may" \
	"smtp_tls_note_starttls_offer=yes"

# TLS server material shared by every inbound service (spec 5.2). auth_only
# guarantees credentials are never accepted before TLS is up on any port.
postconf -e \
	"smtpd_tls_cert_file=${TLS_CERT}" \
	"smtpd_tls_key_file=${TLS_KEY}" \
	"smtpd_tls_security_level=may" \
	"smtpd_tls_auth_only=yes" \
	"smtpd_tls_loglevel=1"

# SASL: Cyrus with the local sasldb2 the panel maintains (spec 5.1). The realm
# is left implicit (smtpd_sasl_local_domain empty) so the authenticated name
# Postfix uses for sender_login_maps is the BARE login the panel writes into the
# map; the sasldb2 lookup still resolves because Postfix hands Cyrus $myhostname
# as the server realm, which equals the realm the accounts were created under.
postconf -e \
	"smtpd_sasl_auth_enable=yes" \
	"smtpd_sasl_type=cyrus" \
	"smtpd_sasl_path=smtpd" \
	"smtpd_sasl_local_domain=" \
	"smtpd_sasl_security_options=noanonymous" \
	"smtpd_sasl_tls_security_options=noanonymous" \
	"broken_sasl_auth_clients=yes"

# Sender binding (spec 5.1 p.3, the critical anti-spoofing control). texthash
# resolves the full address first, then the "@domain" wildcard, so both address
# modes work from the same map.
postconf -e \
	"smtpd_sender_login_maps=texthash:${SENDER_LOGIN_MAPS}" \
	"queue_directory=${QUEUE_DIR}"

# Restrictions: authenticated clients only, no relay to foreign destinations,
# and every authenticated sender address must be owned by its login. NO
# permit_mynetworks anywhere — authorisation is by credentials, never by network
# (spec 5 p.1/p.4, 5.1). This is what makes an open relay impossible.
postconf -e \
	"smtpd_helo_required=yes" \
	"smtpd_relay_restrictions=permit_sasl_authenticated, reject_unauth_destination" \
	"smtpd_recipient_restrictions=permit_sasl_authenticated, reject_unauth_destination" \
	"smtpd_sender_restrictions=reject_sender_login_mismatch, permit"

# Level-1 rate limit by client IP (spec 5 p.5). Backstop that keeps working even
# if the journal-milter (level 2) is down.
postconf -e \
	"smtpd_client_message_rate_limit=${RATE_MSGS}" \
	"anvil_rate_time_unit=${RATE_WINDOW}s"

# Milter chain (spec 5 p.3, 7.3). OpenDKIM signs and is treated strictly
# (default_action=tempfail: if it is unreachable, defer rather than send
# unsigned). The journal-milter is monitoring only and is fail-open
# (default_action=accept): its failure must never block the relay. Per-milter
# settings use Postfix 3.0+ brace syntax.
postconf -e \
	"milter_protocol=6" \
	"milter_default_action=tempfail" \
	"smtpd_milters={ unix:${OPENDKIM_SOCK}, default_action=tempfail }, { unix:${JOURNAL_SOCK}, default_action=accept }" \
	"non_smtpd_milters="

# Bounded milter timeouts (spec 7.3): a *hung* milter (socket accepts but never
# replies) must fail open just like a crash, not stall mail acceptance until the
# Postfix defaults (300s content) elapse. With default_action per milter, a
# journal-milter hang then resolves to accept and an OpenDKIM hang to tempfail,
# but within seconds rather than minutes. Values are well above any healthy
# response time (signing/DB insert are sub-second), so they never fire in normal
# operation.
postconf -e \
	"milter_connect_timeout=${MILTER_CONNECT_TIMEOUT:-15s}" \
	"milter_command_timeout=${MILTER_COMMAND_TIMEOUT:-15s}" \
	"milter_content_timeout=${MILTER_CONTENT_TIMEOUT:-30s}"

# --- master.cf: inbound submission services ----------------------------------
# smtps (465, implicit/wrapper TLS) — the primary, always-on submission service
# (spec 5 p.1). chroot=n so smtpd can read the sasldb2 and sender map under /data
# and the Cyrus config outside any chroot.
postconf -M "smtps/inet=smtps inet n - n - - smtpd"
postconf -P \
	"smtps/inet/smtpd_tls_wrappermode=yes" \
	"smtps/inet/smtpd_sasl_auth_enable=yes" \
	"smtps/inet/smtpd_client_restrictions=permit_sasl_authenticated,reject"

# submission (587, STARTTLS) — optional (spec 5 p.1). Same SASL/milter/limits;
# the only difference is TLS is negotiated via STARTTLS, so require encryption
# before auth. Added only when SUBMISSION_ENABLE=true, otherwise removed so a
# restart after disabling it does not leave the port listening.
if [ "${SUBMISSION_ENABLE}" = "true" ]; then
	postconf -M "submission/inet=submission inet n - n - - smtpd"
	postconf -P \
		"submission/inet/smtpd_tls_security_level=encrypt" \
		"submission/inet/smtpd_sasl_auth_enable=yes" \
		"submission/inet/smtpd_client_restrictions=permit_sasl_authenticated,reject"
else
	postconf -MX "submission/inet" 2>/dev/null || true
fi

# --- inbound smtpd on port 25 (optional inbound relay and/or DMARC ingest) -----
# Debian's stock master.cf enables smtp/inet. When both flags are off, remove
# that listener so port 25 is not an inbound smtpd (outbound delivery uses
# smtp/unix). When either is on: no SASL, no OpenDKIM; relay accepts only
# configured domains/recipients; DMARC accepts only allow-listed report addresses.
if [ "${INBOUND_RELAY_ENABLE}" = "true" ] || [ "${DMARC_REPORTS_ENABLE}" = "true" ]; then
	RELAY_DOMAINS_SETTING=""
	TRANSPORT_SETTING=""
	RECIPIENT_RESTRICTIONS="reject"
	if [ "${INBOUND_RELAY_ENABLE}" = "true" ]; then
		for f in "$RELAY_DOMAINS_MAP" "$TRANSPORT_MAP" "$RELAY_RECIPIENTS_MAP" "$TLS_POLICY_MAP"; do
			[ -e "$f" ] || : > "$f"
		done
		RELAY_DOMAINS_SETTING="texthash:${RELAY_DOMAINS_MAP}"
		TRANSPORT_SETTING="texthash:${TRANSPORT_MAP}"
		RECIPIENT_RESTRICTIONS="reject_unauth_destination,reject_unlisted_recipient"
	fi
	if [ "${DMARC_REPORTS_ENABLE}" = "true" ]; then
		for f in "$DMARC_RECIPIENTS_MAP" "$DMARC_TRANSPORT_MAP" "$DMARC_RELAY_DOMAINS_MAP"; do
			[ -e "$f" ] || : > "$f"
		done
		ln -sf /usr/local/bin/panel /usr/local/bin/dmarc-ingest
		postconf -M "dmarc-ingest/unix-pipe=dmarc-ingest unix - n n - - pipe"
		postconf -P \
			"dmarc-ingest/unix-pipe/flags=Rq" \
			"dmarc-ingest/unix-pipe/user=panel" \
			"dmarc-ingest/unix-pipe/argv=/usr/local/bin/dmarc-ingest"
		if [ -n "$RELAY_DOMAINS_SETTING" ]; then
			RELAY_DOMAINS_SETTING="${RELAY_DOMAINS_SETTING} texthash:${DMARC_RELAY_DOMAINS_MAP}"
			TRANSPORT_SETTING="${TRANSPORT_SETTING} texthash:${DMARC_TRANSPORT_MAP}"
		else
			RELAY_DOMAINS_SETTING="texthash:${DMARC_RELAY_DOMAINS_MAP}"
			TRANSPORT_SETTING="texthash:${DMARC_TRANSPORT_MAP}"
		fi
		RECIPIENT_RESTRICTIONS="check_recipient_access texthash:${DMARC_RECIPIENTS_MAP},${RECIPIENT_RESTRICTIONS}"
	fi
	postconf -e \
		"relay_domains=${RELAY_DOMAINS_SETTING}" \
		"transport_maps=${TRANSPORT_SETTING}" \
		"smtpd_reject_unlisted_recipient=yes"
	if [ "${INBOUND_RELAY_ENABLE}" = "true" ]; then
		postconf -e \
			"relay_recipient_maps=texthash:${RELAY_RECIPIENTS_MAP}" \
			"smtp_tls_policy_maps=texthash:${TLS_POLICY_MAP}"
	else
		postconf -e \
			"relay_recipient_maps=" \
			"smtp_tls_policy_maps="
	fi

	# Milters on port 25. main.cf's chain (OpenDKIM strict + journal-milter) is
	# for the SUBMISSION path only: OpenDKIM must not gate someone else's
	# inbound mail (a signing outage would defer it), and the journal-milter
	# would file inbound mail into the outbound send log under the *sender's*
	# domain — polluting Deliveries and letting a forged From: consume that
	# domain's level-2 rate-limit budget. So smtpd_milters is always overridden
	# here: either the optional antispam milter, or nothing at all.
	INBOUND_MILTERS=""
	if [ "${INBOUND_RELAY_ENABLE}" = "true" ] && [ -n "${INBOUND_ANTISPAM_MILTER}" ]; then
		case "${INBOUND_ANTISPAM_MILTER}" in
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

	postconf -M "smtp/inet=smtp inet n - n - - smtpd"
	postconf -P \
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
else
	postconf -MX "smtp/inet" 2>/dev/null || true
	postconf -MX "dmarc-ingest/unix-pipe" 2>/dev/null || true
	postconf -e \
		"relay_domains=" \
		"transport_maps=" \
		"relay_recipient_maps=" \
		"smtp_tls_policy_maps="
fi

# Disable chroot for every service (spec 5 p.2). Debian ships the smtp delivery
# agent and others chrooted to /var/spool/postfix, where they cannot read
# /etc/resolv.conf — so outbound MX lookups fail with "Host not found" and mail
# never leaves. Inside a container the chroot buys little (the container is the
# isolation boundary) and breaks DNS/TLS trust-store access, so turn it off
# uniformly. Our own smtps/submission services are already n; this covers the
# delivery agents and the rest.
postconf -F "*/*/chroot=n"

# --- Cyrus SASL app config for smtpd -----------------------------------------
# Tells the Cyrus library (invoked by smtpd via smtpd_sasl_path=smtpd) to verify
# passwords straight from the panel-maintained sasldb2 (spec 5.1). PLAIN/LOGIN
# only — both are safe because TLS is mandatory before auth on every port.
mkdir -p /etc/postfix/sasl
cat > /etc/postfix/sasl/smtpd.conf <<EOF
pwcheck_method: auxprop
auxprop_plugin: sasldb
sasldb_path: ${SASLDB_PATH}
mech_list: PLAIN LOGIN
EOF

# Validate the generated configuration; fail loudly if postconf produced
# anything Postfix rejects, before the wrapper tries to start it.
set +e
check_out=$(postfix check 2>&1)
ec=$?
set -e
if [ "$ec" -ne 0 ]; then
	echo "postfix-config: postfix check failed exit $ec" >&2
	printf '%s\n' "$check_out" >&2
	exit "$ec"
fi
