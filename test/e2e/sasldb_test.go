package e2e

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// testSASLDBChangedByRename: the panel changes sasldb2 through a copy renamed
// over it (internal/app, replaceSASLDB), so an AUTH while another
// application's password changes reads the old database or the new one,
// whole. Before, saslpasswd2 wrote in the live file, which Cyrus SASL 2.1.28
// opens with no lock (sasldb/db_berkeley.c, berkeleydb_open()) and smtpd
// reads at every AUTH (_sasldb_getdata()) - assumptions C-S3. A torn read is
// a matter of timing, so this guards the mechanism (the inode changes, the
// mode and group stay) and that nothing fails under the load; it cannot prove
// a race absent.
func testSASLDBChangedByRename(t *testing.T, sc *scenario) {
	churnLogin, churnPassword, err := sc.panel.addApplication(sc.domainID, "churn", "wildcard", "")
	if err != nil {
		t.Fatalf("add application: %v", err)
	}
	churnID, err := sc.panel.applicationID(sc.domainID, churnLogin)
	if err != nil {
		t.Fatal(err)
	}
	stat := func() string {
		t.Helper()
		out, err := h.execIn("selfpost", "stat", "-c", "%i %a %U %G", "/data/sasl/sasldb2")
		if err != nil {
			t.Fatalf("stat sasldb2: %v\n%s", err, out)
		}
		return strings.TrimSpace(out)
	}
	newPassword := func(i int) (string, error) {
		resp, body, err := sc.panel.postForm("/outbound/domains/"+sc.domainID+"/applications/"+churnID+"/password", url.Values{})
		if err != nil || resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("new password %d: %v, status %v\n%.600s", i, err, resp, body)
		}
		pw, ok := extractCodeRow(body, "Password")
		if !ok {
			return "", fmt.Errorf("new password %d: no password on the page\n%.600s", i, body)
		}
		return pw, nil
	}

	// One change, the inode looked at on both sides of it: the copy is made
	// while the old file still exists, so it cannot have the old file's
	// number. (After many changes it may: a freed inode number is reused.)
	before := strings.Fields(stat())
	if churnPassword, err = newPassword(0); err != nil {
		t.Fatal(err)
	}
	after := strings.Fields(stat())
	if len(before) != 4 || len(after) != 4 {
		t.Fatalf("stat sasldb2: %v, %v", before, after)
	}
	if before[0] == after[0] {
		t.Fatalf("sasldb2 kept inode %s across a password change: it was written in place", after[0])
	}

	// Then changes until other AUTHs have run alongside them for a while.
	const wantAuths, maxChanges = 30, 300
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var auths int
	var failures []string
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				err := authOnly(sc.appLogin, sc.appPassword)
				mu.Lock()
				auths++
				if err != nil {
					failures = append(failures, err.Error())
				}
				mu.Unlock()
			}
		}()
	}
	changes := 1
	for ; changes < maxChanges; changes++ {
		pw, err := newPassword(changes)
		if err != nil {
			close(stop)
			wg.Wait()
			t.Fatal(err)
		}
		churnPassword = pw
		mu.Lock()
		enough := auths >= wantAuths
		mu.Unlock()
		if enough {
			break
		}
	}
	close(stop)
	wg.Wait()
	if len(failures) > 0 {
		t.Fatalf("%d of %d AUTHs of %s failed while %s's password changed %d times:\n%s",
			len(failures), auths, sc.appLogin, churnLogin, changes, strings.Join(failures, "\n"))
	}
	t.Logf("%d AUTHs of %s during %d password changes of %s, none refused", auths, sc.appLogin, changes, churnLogin)

	after = strings.Fields(stat())
	if strings.Join(after[1:], " ") != "640 panel selfpost" {
		t.Fatalf("sasldb2 after the changes: mode, owner, group %v; want 640 panel selfpost (Postfix reads it through the group)", after[1:])
	}
	if out, _ := h.execIn("selfpost", "sh", "-c", "ls /data/sasl"); strings.TrimSpace(out) != "sasldb2" {
		t.Fatalf("/data/sasl holds %q, want sasldb2 alone (no copy left behind)", out)
	}
	if err := authOnly(churnLogin, churnPassword); err != nil {
		t.Fatalf("%s with its last password: %v", churnLogin, err)
	}
}

// authOnly connects to the submission port, authenticates and quits.
func authOnly(login, password string) error {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", smtpsAddr, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         selfpostHostname,
	})
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, selfpostHostname)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer c.Close()
	if err := c.Hello("e2e-client.e2e.test"); err != nil {
		return err
	}
	if err := c.Auth(smtp.PlainAuth("", login, password, selfpostHostname)); err != nil {
		return fmt.Errorf("AUTH %s: %w", login, err)
	}
	return c.Quit()
}
