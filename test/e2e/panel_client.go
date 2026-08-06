package e2e

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// panelBaseURL is where compose.override.yml publishes the panel's plain-HTTP
// port (PANEL_COOKIE_SECURE=false — no reverse proxy in this stand).
const panelBaseURL = "http://127.0.0.1:20080"

// panelClient drives the panel exactly as a browser would: an HTTP client
// with a cookie jar, HTML forms posted as the templates render them, and
// responses scraped for the bits a later step needs (a domain's id, an
// application's one-shot password, the DKIM record to publish). This is
// deliberate — it is also the only way to prove that what the panel tells an
// administrator to publish is the record that actually verifies (plan C.4).
type panelClient struct {
	http *http.Client
}

func newPanelClient() (*panelClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &panelClient{http: &http.Client{
		Jar:     jar,
		Timeout: 15 * time.Second,
	}}, nil
}

// readSetupToken reads the one-time setup URL SelfPost wrote to /data (bind
// mounted at stageDir/data/setup-token) and returns just the token.
func readSetupToken(stageDir string) (string, error) {
	var raw []byte
	err := waitFor("setup-token to appear", 30*time.Second, 300*time.Millisecond, func() (bool, error) {
		b, err := os.ReadFile(filepath.Join(stageDir, "data", "setup-token"))
		if err != nil {
			return false, err
		}
		raw = b
		return len(b) > 0, nil
	})
	if err != nil {
		return "", err
	}
	u, err := url.Parse(strings.TrimSpace(string(raw)))
	if err != nil {
		return "", fmt.Errorf("parse setup token file: %w", err)
	}
	return strings.TrimPrefix(u.Path, "/setup/"), nil
}

func (c *panelClient) postForm(path string, form url.Values) (*http.Response, string, error) {
	resp, err := c.http.PostForm(panelBaseURL+path, form)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp, string(body), err
}

func (c *panelClient) get(path string) (*http.Response, string, error) {
	resp, err := c.http.Get(panelBaseURL + path)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp, string(body), err
}

// setup creates the administrator through the one-time /setup/<token> form.
func (c *panelClient) setup(token, username, password string) error {
	resp, body, err := c.postForm("/setup/"+token, url.Values{
		"username":         {username},
		"password":         {password},
		"password_confirm": {password},
	})
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("setup: status %d: %s", resp.StatusCode, body)
	}
	return nil
}

// login authenticates and stores the session cookie in the client's jar.
func (c *panelClient) login(username, password string) error {
	resp, body, err := c.postForm("/login", url.Values{
		"username": {username},
		"password": {password},
	})
	if err != nil {
		return err
	}
	if resp.Request.URL.Path != "/status" {
		return fmt.Errorf("login did not land on /status (landed on %s): %s", resp.Request.URL.Path, body)
	}
	return nil
}

// addDomain submits the add-domain form and returns its assigned id, read
// back from the redirect target /domains/{id}.
func (c *panelClient) addDomain(name string) (string, error) {
	resp, body, err := c.postForm("/domains", url.Values{"name": {name}})
	if err != nil {
		return "", err
	}
	m := regexp.MustCompile(`^/domains/(\d+)$`).FindStringSubmatch(resp.Request.URL.Path)
	if m == nil {
		return "", fmt.Errorf("add domain %q: unexpected landing page %s: %s", name, resp.Request.URL.Path, body)
	}
	return m[1], nil
}

// dkimRecord fetches a domain's page and scrapes the DKIM DNS record it tells
// the administrator to publish.
func (c *panelClient) dkimRecord(domainID string) (name, value string, err error) {
	_, body, err := c.get("/domains/" + domainID)
	if err != nil {
		return "", "", err
	}
	name, ok := extractCodeRow(body, "Host / name")
	if !ok {
		return "", "", fmt.Errorf("could not find DKIM record name on domain page")
	}
	value, ok = extractCodeRow(body, "Value")
	if !ok {
		return "", "", fmt.Errorf("could not find DKIM record value on domain page")
	}
	return name, value, nil
}

// addApplication submits the add-application form and returns the one-shot
// login/password the panel renders inline (security.md — never recoverable
// later, so this is the only place to read it).
func (c *panelClient) addApplication(domainID, login, mode, addresses string) (appLogin, password string, err error) {
	resp, body, err := c.postForm("/domains/"+domainID+"/applications", url.Values{
		"login":     {login},
		"mode":      {mode},
		"addresses": {addresses},
	})
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("add application %q: status %d: %s", login, resp.StatusCode, body)
	}
	appLogin, ok := extractCodeRow(body, "Login")
	if !ok {
		return "", "", fmt.Errorf("could not find new application login in response")
	}
	password, ok = extractCodeRow(body, "Password")
	if !ok {
		return "", "", fmt.Errorf("could not find new application password in response")
	}
	return appLogin, password, nil
}

// setRateLimit saves a level-2 differentiated limit (README § Rate limiting)
// on either a domain (/domains/{id}/ratelimit) or an application
// (/applications/{id}/ratelimit).
func (c *panelClient) setRateLimit(path, allowedIP string, maxMessages, windowSeconds int) error {
	resp, body, err := c.postForm(path, url.Values{
		"allowed_ips":    {allowedIP},
		"max_messages":   {fmt.Sprintf("%d", maxMessages)},
		"window_seconds": {fmt.Sprintf("%d", windowSeconds)},
	})
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("set rate limit: status %d: %s", resp.StatusCode, body)
	}
	return nil
}

// sendLogRows returns the raw /deliveries/rows HTML fragment, filtered to one
// domain, for polling a row's status without parsing full HTML into structs.
func (c *panelClient) sendLogRows(domain string) (string, error) {
	_, body, err := c.get("/deliveries/rows?domain=" + url.QueryEscape(domain))
	return body, err
}

// status fetches the authenticated landing page — used after a container
// restart to confirm the session cookie is still accepted (plan C.4 check 8).
func (c *panelClient) status() (*http.Response, error) {
	resp, _, err := c.get("/status")
	return resp, err
}

// applicationID scrapes an application's numeric id off its domain page row,
// keyed by login — needed to build /applications/{id}/ratelimit, which the
// add-application response (just the login/password) does not carry.
func (c *panelClient) applicationID(domainID, login string) (string, error) {
	_, body, err := c.get("/domains/" + domainID)
	if err != nil {
		return "", err
	}
	pattern := `(?s)<td class="code">` + regexp.QuoteMeta(login) + `</td>.*?/applications/(\d+)/mode`
	m := regexp.MustCompile(pattern).FindStringSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("could not find application id for login %q", login)
	}
	return m[1], nil
}

// extractCodeRow scrapes the value of a "<label>LABEL</label> ... <span
// class=\"code\">VALUE</span>" pair from a rendered panel page (see
// internal/web/templates/domain_detail.html). It is deliberately anchored to
// the label text rather than position, so it survives unrelated template
// reordering. html/template's escaper is conservative about which characters
// it entity-encodes in text nodes — a DKIM value's base64 "+" comes back as
// "&#43;" — so the match is HTML-unescaped before returning.
func extractCodeRow(body, label string) (string, bool) {
	pattern := `<label>` + regexp.QuoteMeta(label) + `</label>\s*<div class="code-row">\s*<span class="code">([^<]*)</span>`
	m := regexp.MustCompile(pattern).FindStringSubmatch(body)
	if m == nil {
		return "", false
	}
	return html.UnescapeString(strings.TrimSpace(m[1])), true
}
