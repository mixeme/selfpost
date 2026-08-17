package main

import (
	"slices"
	"testing"
)

// documentedPublic matches docs/guide.md "Environment variables" table.
var documentedPublic = []string{
	"SELFPOST_HOSTNAME",
	"SUBMISSION_ENABLE",
	"RATE_LIMIT_MESSAGES_PER_IP",
	"RATE_LIMIT_WINDOW_SECONDS",
	"SEND_LOG_RETENTION_DAYS",
	"PANEL_SESSION_IDLE_DAYS",
	"SELFPOST_DNS_RESOLVERS",
	"TRUSTED_PROXY_CIDR",
	"INBOUND_RELAY_ENABLE",
	"INBOUND_ANTISPAM_MILTER",
	"INBOUND_ANTISPAM_MILTER_ACTION",
	"INBOUND_RATE_LIMIT_MESSAGES_PER_IP",
	"INBOUND_MESSAGE_SIZE_LIMIT",
}

// documentedInternal matches architecture.md § Configuration "Internal env vars".
var documentedInternal = []string{
	"SELFPOST_DATA_DIR",
	"SELFPOST_DB_PATH",
	"SELFPOST_SETUP_TOKEN_FILE",
	"PANEL_HTTP_ADDR",
	"JOURNAL_MILTER_SOCKET",
	"MAIL_LOG",
	"PANEL_COOKIE_SECURE",
	"OPENDKIM_SOCKET",
	"OPENDKIM_DIR",
	"DKIM_SELECTOR_DEFAULT",
	"SASL_DB_PATH",
	"SASL_REALM",
	"POSTFIX_DIR",
	"POSTFIX_SENDER_LOGIN_MAPS",
	"POSTFIX_QUEUE_DIR",
	"POSTFIX_RELAY_DOMAINS",
	"POSTFIX_TRANSPORT_MAPS",
	"POSTFIX_RELAY_RECIPIENTS",
	"POSTFIX_TLS_POLICY_MAPS",
	"SELFPOST_DEPLOY_ROOT",
	"MILTER_CONNECT_TIMEOUT",
	"MILTER_COMMAND_TIMEOUT",
	"MILTER_CONTENT_TIMEOUT",
	"MILTER_WAIT_TIMEOUT",
	"TLS_RELOAD_INTERVAL_SECONDS",
	"LOGROTATE_INTERVAL_SECONDS",
}

// documentedComposeFixed are env keys documented outside the table (TLS paths fixed in compose).
var documentedComposeFixed = []string{
	"TLS_CERT_FILE",
	"TLS_KEY_FILE",
}

// loadConfigKeys is every environment variable read by loadConfig() in main.go.
// Update together with loadConfig when adding a new key.
var loadConfigKeys = []string{
	"SELFPOST_DATA_DIR",
	"PANEL_HTTP_ADDR",
	"JOURNAL_MILTER_SOCKET",
	"MAIL_LOG",
	"SEND_LOG_RETENTION_DAYS",
	"SELFPOST_DB_PATH",
	"SELFPOST_SETUP_TOKEN_FILE",
	"SELFPOST_HOSTNAME",
	"PANEL_COOKIE_SECURE",
	"SUBMISSION_ENABLE",
	"TRUSTED_PROXY_CIDR",
	"PANEL_SESSION_IDLE_DAYS",
	"SELFPOST_DNS_RESOLVERS",
	"TLS_CERT_FILE",
	"OPENDKIM_SOCKET",
	"OPENDKIM_DIR",
	"DKIM_SELECTOR_DEFAULT",
	"SASL_DB_PATH",
	"SASL_REALM",
	"POSTFIX_DIR",
	"SELFPOST_DEPLOY_ROOT",
	"INBOUND_RELAY_ENABLE",
}

// buildScriptKeys is every ${VAR:-…} / os.Getenv used in build/*.sh and entrypoint.sh
// but not necessarily in loadConfig. Update when startup scripts gain a new knob.
var buildScriptKeys = []string{
	"SELFPOST_HOSTNAME",
	"TLS_CERT_FILE",
	"TLS_KEY_FILE",
	"RATE_LIMIT_MESSAGES_PER_IP",
	"RATE_LIMIT_WINDOW_SECONDS",
	"OPENDKIM_SOCKET",
	"JOURNAL_MILTER_SOCKET",
	"MAIL_LOG",
	"POSTFIX_SENDER_LOGIN_MAPS",
	"POSTFIX_QUEUE_DIR",
	"SASL_DB_PATH",
	"SUBMISSION_ENABLE",
	"INBOUND_RELAY_ENABLE",
	"INBOUND_ANTISPAM_MILTER",
	"INBOUND_ANTISPAM_MILTER_ACTION",
	"INBOUND_RATE_LIMIT_MESSAGES_PER_IP",
	"INBOUND_MESSAGE_SIZE_LIMIT",
	"POSTFIX_RELAY_DOMAINS",
	"POSTFIX_TRANSPORT_MAPS",
	"POSTFIX_RELAY_RECIPIENTS",
	"POSTFIX_TLS_POLICY_MAPS",
	"MILTER_CONNECT_TIMEOUT",
	"MILTER_COMMAND_TIMEOUT",
	"MILTER_CONTENT_TIMEOUT",
	"MILTER_WAIT_TIMEOUT",
	"TLS_RELOAD_INTERVAL_SECONDS",
	"LOGROTATE_INTERVAL_SECONDS",
}

func documentedKeys() []string {
	keys := append([]string{}, documentedPublic...)
	keys = append(keys, documentedInternal...)
	keys = append(keys, documentedComposeFixed...)
	slices.Sort(keys)
	return slices.Compact(keys)
}

func TestLoadConfigKeysDocumented(t *testing.T) {
	doc := documentedKeys()
	for _, key := range loadConfigKeys {
		if !slices.Contains(doc, key) {
			t.Errorf("loadConfig reads %s but it is not listed in guide.md public, internal, or compose-fixed env docs", key)
		}
	}
}

func TestBuildScriptKeysDocumented(t *testing.T) {
	doc := documentedKeys()
	for _, key := range buildScriptKeys {
		if !slices.Contains(doc, key) {
			t.Errorf("build scripts read %s but it is not listed in guide.md env documentation", key)
		}
	}
}

func TestDocumentedKeysAreRead(t *testing.T) {
	read := append([]string{}, loadConfigKeys...)
	read = append(read, buildScriptKeys...)
	slices.Sort(read)
	read = slices.Compact(read)

	for _, key := range documentedKeys() {
		if !slices.Contains(read, key) {
			t.Errorf("guide.md documents %s but no code in loadConfig or build scripts reads it", key)
		}
	}
}
