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

# --- environment knobs (spec 8) ----------------------------------------------
# Server hostname: used as HELO name AND, crucially, as the Cyrus SASL realm the
# sasldb2 accounts are looked up under. The panel creates accounts under realm
# $SELFPOST_HOSTNAME (SASL_REALM), so myhostname MUST match or authentication
# fails. Fall back to the container hostname only outside a real deployment.
HOSTNAME_VALUE="${SELFPOST_HOSTNAME:-$(hostname -f 2>/dev/null || hostname)}"

# TLS material supplied by the reverse-proxy through a read-only bind mount
# (spec 5.2). The relay requires TLS on 465; if these files are absent the
# master still starts but TLS handshakes on 465 fail until they appear.
TLS_CERT="${TLS_CERT_FILE:-/etc/postfix/tls/fullchain.pem}"
TLS_KEY="${TLS_KEY_FILE:-/etc/postfix/tls/privkey.pem}"

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
SASLDB_PATH="${SASL_DB_PATH:-/data/sasl/sasldb2}"

# Optional submission service on 587 (spec 5 p.1: off by default, enabled only
# when a client library needs STARTTLS on 587 instead of implicit TLS on 465).
SUBMISSION_ENABLE="${SUBMISSION_ENABLE:-false}"

# --- main.cf -----------------------------------------------------------------
postconf -e \
	"myhostname=${HOSTNAME_VALUE}" \
	"maillog_file=/var/log/mail.log" \
	"mydestination=" \
	"relayhost=" \
	"inet_interfaces=all" \
	"inet_protocols=all"

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
	"smtpd_sender_login_maps=texthash:${SENDER_LOGIN_MAPS}"

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
# if the journal-milter (level 2, Phase 8) is down.
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
postfix check
