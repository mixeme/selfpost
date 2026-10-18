package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
)

// The two queues are one Postfix queue split by the recipient's domain. Their
// markup is held to the mockups by the guards in internal/web/view; what is
// checked here is the split, the link to the send-log row, and who may look.

// stubQueue answers queueListing with msgs (or err) for the test.
func stubQueue(t *testing.T, msgs []postfix.QueuedMessage, err error) {
	t.Helper()
	old := queueListing
	t.Cleanup(func() { queueListing = old })
	queueListing = func() ([]postfix.QueuedMessage, error) { return msgs, err }
}

func TestQueuesSplitByRecipientDomain(t *testing.T) {
	s := newInboundLogStand(t) // inbound domains a.example.com, b.example.com
	if err := s.st.InsertQueued(store.SendLogEntry{QueueID: "OUT00001", Domain: "shop.example.org", AppLogin: "shop",
		From: "alerts@shop.example.org", To: "noc@example.net", Subject: "Hi"}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.st.QuerySendLog(store.SendLogFilter{AllDomains: true}, 1, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v (%d rows)", err, len(rows))
	}
	at := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	stubQueue(t, []postfix.QueuedMessage{
		{ID: "OUT00001", Queue: "deferred", Sender: "alerts@shop.example.org", Arrival: at, Size: 3174,
			Recipients: []postfix.QueuedRecipient{{Address: "NOC@example.net", DelayReason: "451 4.7.1 Greylisted"}}},
		{ID: "IN000001", Queue: "deferred", Sender: "someone@example.net", Arrival: at, Size: 5000,
			Recipients: []postfix.QueuedRecipient{{Address: "alice@A.example.com", DelayReason: "connect to upstream.example.com[192.0.2.7]:25: Connection refused"}}},
		{ID: "NOTE0001", Queue: "active", Sender: "postmaster@mail.example.org", Arrival: at, Size: 900,
			Recipients: []postfix.QueuedRecipient{{Address: "admin@example.net"}}},
	}, nil)

	out := s.get(s.h.HandleMailQueue, "/outbound/queue", globalPrincipal).Body.String()
	has(t, "Outbound queue", out, "OUT00001", "451 4.7.1 Greylisted",
		`<a href="/outbound/log/`+strconv.FormatInt(rows[0].ID, 10)+`">Details</a>`, // matched without regard to case
		"NOTE0001", "Being delivered now", "2 messages")
	if strings.Contains(out, "IN000001") {
		t.Error("a recipient of an inbound domain is on the Outbound queue")
	}
	if strings.Count(out, ">Details</a>") != 1 {
		t.Error("the panel's own message has no send-log row and should have no Details link")
	}

	in := s.get(s.h.HandleInboundQueue, "/inbound/queue", globalPrincipal).Body.String()
	has(t, "Inbound queue", in, "IN000001", "Connection refused", "1 message · 4.9 kB")
	for _, other := range []string{"OUT00001", "NOTE0001", ">Details</a>"} {
		if strings.Contains(in, other) {
			t.Errorf("the Inbound queue shows %q", other)
		}
	}

	frag := s.get(s.h.HandleInboundQueueBody, "/inbound/queue/fragment", globalPrincipal).Body.String()
	if !strings.HasPrefix(frag, `<div class="box" id="in-queue" data-poll`) || !strings.Contains(frag, "IN000001") {
		t.Errorf("the Inbound queue's fragment is not its polled box:\n%.300s", frag)
	}
}

// A queue that cannot be read says so on both pages, and never reads as empty.
func TestQueuesSayWhenTheyCannotBeRead(t *testing.T) {
	s := newInboundLogStand(t)
	stubQueue(t, nil, errors.New("postqueue: Permission denied"))
	for _, page := range []struct {
		name string
		h    http.HandlerFunc
	}{{"/outbound/queue", s.h.HandleMailQueue}, {"/inbound/queue", s.h.HandleInboundQueue}} {
		body := s.get(page.h, page.name, globalPrincipal).Body.String()
		has(t, page.name, body, "Could not read the mail queue.")
		if strings.Contains(body, "Nothing is waiting") || strings.Contains(body, "Permission denied") {
			t.Errorf("%s reads as empty or shows the error itself", page.name)
		}
	}
}

// Both queues are the global role's: a domain administrator is answered 404,
// whatever they reach; with the relay off the Inbound queue does not exist.
func TestQueuesAreForTheGlobalRoleOnly(t *testing.T) {
	s := newInboundLogStand(t)
	stubQueue(t, nil, nil)
	ops := s.owner(t, "owns-a", s.a)
	all := userWith(t, s.st, "owns-all", store.RoleDomain, store.Reach{AllInbound: true, AllDomains: true})
	for _, p := range []auth.Principal{ops, all} {
		for target, h := range map[string]http.HandlerFunc{
			"/inbound/queue": s.h.HandleInboundQueue, "/inbound/queue/fragment": s.h.HandleInboundQueueBody,
			"/outbound/queue": s.h.HandleMailQueue, "/outbound/queue/fragment": s.h.HandleMailQueueBody,
		} {
			if rec := s.get(h, target, p); rec.Code != http.StatusNotFound {
				t.Errorf("GET %s as %s = %d, want 404", target, p.Username, rec.Code)
			}
		}
	}
	s.h.cfg.InboundEnabled = false
	if rec := s.get(s.h.HandleInboundQueue, "/inbound/queue", globalPrincipal); rec.Code != http.StatusNotFound {
		t.Errorf("GET /inbound/queue with the relay off = %d, want 404", rec.Code)
	}
}
