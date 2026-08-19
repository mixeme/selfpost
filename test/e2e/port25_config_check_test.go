package e2e

import "testing"

// realMasterCfSample is captured verbatim (via `cat -A`) from a container
// generated with INBOUND_RELAY_ENABLE=false DMARC_REPORTS_ENABLE=true: after
// the unconditional `postconf -F "*/*/chroot=n"` rewraps the file, the
// smtp/inet stanza's options are spread across several continuation lines
// that split values mid-token and re-flow what were originally separate
// "-o " lines into one paragraph. Confirms parseSMTPInetOptions reconstructs
// the logical options from that rather than a plain single-option-per-line
// master.cf.
const realMasterCfSample = `smtp       inet  n       -       n       -       -       smtpd
    -o smtpd_milters=
    -o smtpd_recipient_restrictions=check_recipient_access
    texthash:/data/postfix/dmarc_recipients,reject -o smtpd_sasl_auth_enable=no
    -o smtpd_tls_auth_only=no -o smtpd_sender_login_maps= -o
    smtpd_sender_restrictions= -o smtpd_client_restrictions= -o
    smtpd_relay_restrictions=reject_unauth_destination -o
    smtpd_client_message_rate_limit=20 -o message_size_limit=5242880
pickup     unix  n       -       n       60      1       pickup
`

func TestParseSMTPInetOptionsAcrossContinuationLines(t *testing.T) {
	stanza := smtpInetStanza(realMasterCfSample)
	opts := parseSMTPInetOptions(stanza)

	want := map[string]string{
		"smtpd_milters":                   "",
		"smtpd_recipient_restrictions":    "check_recipient_access texthash:/data/postfix/dmarc_recipients,reject",
		"smtpd_sasl_auth_enable":          "no",
		"smtpd_tls_auth_only":             "no",
		"smtpd_sender_login_maps":         "",
		"smtpd_sender_restrictions":       "",
		"smtpd_client_restrictions":       "",
		"smtpd_relay_restrictions":        "reject_unauth_destination",
		"smtpd_client_message_rate_limit": "20",
		"message_size_limit":              "5242880",
	}
	for param, wantVal := range want {
		if got, ok := opts[param]; !ok || got != wantVal {
			t.Errorf("opts[%q] = %q (present=%v), want %q", param, got, ok, wantVal)
		}
	}
	if len(opts) != len(want) {
		t.Errorf("parsed %d options, want %d: %+v", len(opts), len(want), opts)
	}
}

func TestSMTPInetStanzaSkipsDeliveryAgent(t *testing.T) {
	master := "smtp       unix  -       -       n       -       -       smtp\n" +
		"#       -o smtp_helo_timeout=5\n" +
		realMasterCfSample
	stanza := smtpInetStanza(master)
	if stanza == "" {
		t.Fatal("no smtp/inet stanza found")
	}
	opts := parseSMTPInetOptions(stanza)
	if v, ok := opts["smtpd_milters"]; !ok || v != "" {
		t.Fatalf("picked up the wrong \"smtp\" service: opts = %+v", opts)
	}
}
