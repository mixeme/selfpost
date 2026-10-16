package auth

import (
	"net"
	"net/http/httptest"
	"testing"
)

// Health says where the panel sees a request from: through a trusted proxy,
// the client's own address from X-Forwarded-For; through a proxy it does not
// trust, the proxy's, with the header left unread - every visitor then
// shares one sign-in limit; and with no header, the connection's own.
func TestSeenAddress(t *testing.T) {
	_, proxyNet, _ := net.ParseCIDR("172.18.0.0/16")
	m := &Module{trustedProxies: []*net.IPNet{proxyNet}}
	untrusting := &Module{}

	r := httptest.NewRequest("GET", "/server/health", nil)
	r.RemoteAddr = "172.18.0.1:40112"
	r.Header.Set("X-Forwarded-For", "198.51.100.7")
	if s := m.Seen(r); s != (SeenAddress{Client: "198.51.100.7", Peer: "172.18.0.1", FromHeader: true}) {
		t.Errorf("through a trusted proxy: %+v", s)
	}
	if s := untrusting.Seen(r); s != (SeenAddress{Client: "172.18.0.1", Peer: "172.18.0.1", HeaderIgnored: true}) {
		t.Errorf("through a proxy not trusted: %+v", s)
	}

	r.Header.Del("X-Forwarded-For")
	r.RemoteAddr = "203.0.113.9:5000"
	if s := m.Seen(r); s != (SeenAddress{Client: "203.0.113.9", Peer: "203.0.113.9"}) {
		t.Errorf("direct: %+v", s)
	}
}
