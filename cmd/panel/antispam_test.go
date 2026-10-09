package main

import "testing"

// The panel reads the inbound filter's two variables with the semantics of
// build/postfix-config.sh: the action is accept unless it says tempfail, and a
// value the script would refuse is not guessed at.
func TestAntispamActionMirrorsTheBuildScript(t *testing.T) {
	for raw, want := range map[string]string{
		"": "accept", "accept": "accept", "tempfail": "tempfail",
		"reject": "", "Accept": "", "TEMPFAIL": "", " accept": "",
	} {
		if got := antispamAction(raw); got != want {
			t.Errorf("antispamAction(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestLoadConfigReadsTheInboundFilter(t *testing.T) {
	t.Setenv("INBOUND_RELAY_ENABLE", "true")
	t.Setenv("INBOUND_ANTISPAM_MILTER", "inet:antispam:11332")
	t.Setenv("INBOUND_ANTISPAM_MILTER_ACTION", "tempfail")
	cfg := loadConfig()
	if !cfg.inboundEnabled || cfg.inboundAntispamMilter != "inet:antispam:11332" || cfg.inboundAntispamAction != "tempfail" {
		t.Errorf("config = %+v", cfg)
	}

	t.Setenv("INBOUND_ANTISPAM_MILTER", "")
	t.Setenv("INBOUND_ANTISPAM_MILTER_ACTION", "")
	cfg = loadConfig()
	if cfg.inboundAntispamMilter != "" || cfg.inboundAntispamAction != "accept" {
		t.Errorf("unset filter: milter %q, action %q", cfg.inboundAntispamMilter, cfg.inboundAntispamAction)
	}
}
