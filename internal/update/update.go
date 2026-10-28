// Package update asks ghcr.io, once a day, which versions of SelfPost are
// published, so that Server › Health can say when one newer than the running
// version is out (card 10 Version; owner, 2026-10-28: the source is the image
// registry, the check is on unless SELFPOST_UPDATE_CHECK=false).
//
// A check is two anonymous GET requests to ghcr.io: a pull token for the
// public repository, then the repository's tag list. They carry nothing about
// the installation - no version, no host name, no cookie. A published version
// is a tag of the form X.Y.Z: the per-architecture tags (X.Y.Z-amd64) and
// anything else are skipped, and a version whose release failed has no tag
// there at all (2.16.9), so what is shown is what `docker compose pull` gets.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Registry and Repository name the image the guide pins
// (deploy/docker-compose.yml).
const (
	Registry   = "https://ghcr.io"
	Repository = "mixeme/selfpost"
)

// Interval is how often the registry is asked; RetryAfter is the wait after
// a check that failed, shorter so that a network outage at start-up does not
// leave the card unknown for a day.
const (
	Interval   = 24 * time.Hour
	RetryAfter = time.Hour
)

// maxPages bounds the tag list's pagination (OCI distribution spec, "Listing
// Tags": a Link header names the next page). The list holds three tags per
// release; one page of 1000 holds them all for a long time.
const maxPages = 10

// State is what the card shows.
type State struct {
	// Running is the version of this binary (buildinfo.Version).
	Running string
	// Published is the newest X.Y.Z tag in the registry; "" until a check
	// has succeeded.
	Published string
	// Checked is when the registry last answered.
	Checked time.Time
	// Err is why the last attempt failed; "" when it did not.
	Err string
	// Off is SELFPOST_UPDATE_CHECK=false: the registry is never asked.
	Off bool
}

// Newer reports whether a published version is newer than the running one.
// A running version that is not X.Y.Z (a local build, "dev", "e2e") is never
// behind: there is nothing to compare it with.
func (s State) Newer() bool {
	run, ok := Parse(s.Running)
	if !ok {
		return false
	}
	pub, ok := Parse(s.Published)
	return ok && pub.After(run)
}

// Version is X.Y.Z.
type Version [3]int

// Parse reads X.Y.Z, nothing before or after it.
func Parse(s string) (Version, bool) {
	var v Version
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// After reports whether v is a later version than w.
func (v Version) After(w Version) bool {
	for i := range v {
		if v[i] != w[i] {
			return v[i] > w[i]
		}
	}
	return false
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// Newest is the latest X.Y.Z among tags; "" when there is none.
func Newest(tags []string) string {
	var best Version
	found := false
	for _, t := range tags {
		if v, ok := Parse(t); ok && (!found || v.After(best)) {
			best, found = v, true
		}
	}
	if !found {
		return ""
	}
	return best.String()
}

// Checker keeps the last answer of the registry. A nil Checker is a check
// that is off.
type Checker struct {
	running  string
	registry string
	client   *http.Client

	mu    sync.Mutex
	state State
}

// New is a check of the image registry for a binary of version running.
func New(running string) *Checker {
	return &Checker{
		running:  running,
		registry: Registry,
		client:   &http.Client{Timeout: 20 * time.Second},
		state:    State{Running: running},
	}
}

// State is the last reading; for a nil Checker, the check that is off.
func (c *Checker) State(running string) State {
	if c == nil {
		return State{Running: running, Off: true}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Run asks the registry now and then once an Interval - after a failure,
// once a RetryAfter - until ctx ends.
func (c *Checker) Run(ctx context.Context) error {
	for {
		wait := Interval
		if err := c.Check(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("update check: %v; asking again in %s", err, RetryAfter)
			wait = RetryAfter
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// Check asks the registry once and keeps what it said. A failure keeps the
// version last published and when it was read: an answer of yesterday is
// still the newest the panel knows.
func (c *Checker) Check(ctx context.Context) error {
	tags, err := c.tags(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.state.Err = err.Error()
		return err
	}
	c.state.Published, c.state.Checked, c.state.Err = Newest(tags), time.Now(), ""
	return nil
}

// tags is the repository's tag list: an anonymous pull token
// (GET /token?scope=repository:<repo>:pull), then GET /v2/<repo>/tags/list
// with it, following the Link header's next page.
func (c *Checker) tags(ctx context.Context) ([]string, error) {
	var tok struct {
		Token string `json:"token"`
	}
	tokenURL := c.registry + "/token?scope=" + url.QueryEscape("repository:"+Repository+":pull")
	if err := c.getJSON(ctx, tokenURL, "", &tok, nil); err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	if tok.Token == "" {
		return nil, errors.New("token: the registry gave none")
	}
	var all []string
	next := c.registry + "/v2/" + Repository + "/tags/list?n=1000"
	for page := 0; next != "" && page < maxPages; page++ {
		var list struct {
			Tags []string `json:"tags"`
		}
		var link string
		if err := c.getJSON(ctx, next, tok.Token, &list, &link); err != nil {
			return nil, fmt.Errorf("tag list: %w", err)
		}
		all = append(all, list.Tags...)
		next = nextPage(c.registry, link)
	}
	return all, nil
}

func (c *Checker) getJSON(ctx context.Context, u, bearer string, out any, link *string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	if link != nil {
		*link = resp.Header.Get("Link")
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// nextPage reads `<url>; rel="next"`; a relative URL is the registry's.
func nextPage(registry, link string) string {
	for _, part := range strings.Split(link, ",") {
		target, params, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok || !strings.Contains(strings.ReplaceAll(params, " ", ""), `rel="next"`) {
			continue
		}
		target = strings.Trim(strings.TrimSpace(target), "<>")
		if strings.HasPrefix(target, "/") {
			return registry + target
		}
		if strings.HasPrefix(target, registry+"/") {
			return target
		}
	}
	return ""
}
