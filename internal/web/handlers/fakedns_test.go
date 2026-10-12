package handlers

import (
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeZone is a DNS server on the loopback that answers from a table, so that a
// page can be rendered against records that are published, wrong or absent
// without leaving the machine. Names are lower case without the trailing dot;
// a name the zone does not hold is NXDOMAIN, which is how a record that is not
// published looks to the checker.
type fakeZone struct {
	mu   sync.Mutex
	txt  map[string][]string
	addr map[string]string // name -> IPv4 address
}

// newFakeZone starts the server for the length of the test and returns the
// resolver address to give dnscheck.New.
func newFakeZone(t *testing.T) (*fakeZone, string) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake DNS: listen: %v", err)
	}
	z := &fakeZone{txt: map[string][]string{}, addr: map[string]string{}}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if resp := z.answer(buf[:n]); resp != nil {
				_, _ = conn.WriteTo(resp, from)
			}
		}
	}()
	return z, conn.LocalAddr().String()
}

// publishTXT sets the TXT record of a name; publishA its IPv4 address.
func (z *fakeZone) publishTXT(name, value string) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.txt[strings.ToLower(name)] = append(z.txt[strings.ToLower(name)], value)
}

func (z *fakeZone) publishA(name, ip string) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.addr[strings.ToLower(name)] = ip
}

// answer builds the reply to one query: the question is copied back, then the
// records of the asked type if the zone has any. A name with nothing of the
// asked type but known under another type is an empty answer (NOERROR), an
// unknown name is NXDOMAIN.
func (z *fakeZone) answer(q []byte) []byte {
	if len(q) < 12 || binary.BigEndian.Uint16(q[4:6]) != 1 {
		return nil
	}
	i, labels := 12, []string{}
	for {
		if i >= len(q) {
			return nil
		}
		l := int(q[i])
		if l == 0 {
			i++
			break
		}
		if i+1+l > len(q) {
			return nil
		}
		labels = append(labels, string(q[i+1:i+1+l]))
		i += 1 + l
	}
	if i+4 > len(q) {
		return nil
	}
	name, qtype := strings.ToLower(strings.Join(labels, ".")), binary.BigEndian.Uint16(q[i:i+2])
	question := q[12 : i+4]

	z.mu.Lock()
	txt, ip := z.txt[name], z.addr[name]
	z.mu.Unlock()

	var answers [][]byte
	switch {
	case qtype == 16:
		for _, v := range txt {
			var rdata []byte
			for len(v) > 255 {
				rdata = append(rdata, 255)
				rdata = append(rdata, v[:255]...)
				v = v[255:]
			}
			rdata = append(rdata, byte(len(v)))
			rdata = append(rdata, v...)
			answers = append(answers, record(16, rdata))
		}
	case qtype == 1 && ip != "":
		answers = append(answers, record(1, net.ParseIP(ip).To4()))
	}
	rcode := uint16(0)
	if len(txt) == 0 && ip == "" {
		rcode = 3
	}
	resp := make([]byte, 12, 512)
	copy(resp, q[:2])
	binary.BigEndian.PutUint16(resp[2:], 0x8180|rcode)
	binary.BigEndian.PutUint16(resp[4:], 1)
	binary.BigEndian.PutUint16(resp[6:], uint16(len(answers)))
	resp = append(resp, question...)
	for _, a := range answers {
		resp = append(resp, a...)
	}
	return resp
}

// record is one answer record named by the question (a pointer to offset 12),
// class IN, with a TTL of a minute.
func record(rtype uint16, rdata []byte) []byte {
	r := []byte{0xC0, 0x0C, 0, 0, 0, 1, 0, 0, 0, 60, 0, 0}
	binary.BigEndian.PutUint16(r[2:], rtype)
	binary.BigEndian.PutUint16(r[10:], uint16(len(rdata)))
	return append(r, rdata...)
}
