package web

import (
	"net/http"
	"net/url"
)

// contentSecurityPolicy is the panel's CSP. Everything the pages load —
// stylesheet, HTMX, the panel's own script, the favicon — is served from
// /static by this same origin, and no template carries an inline <script>,
// an inline event handler or a style="..." attribute (the
// template guard test enforces that), so 'self' needs no exemptions:
//
//   - default-src 'self' covers scripts, styles, images and the XHR that
//     HTMX's polling fragments use;
//   - object-src/frame-ancestors 'none' remove plugin embedding and framing
//     (clickjacking) outright;
//   - base-uri 'none' stops an injected <base> from re-pointing every
//     relative URL on the page, including the form actions;
//   - form-action 'self' keeps a form from being retargeted at another host.
//
// This is a second line of defence: XSS is already prevented by
// html/template's contextual auto-escaping (spec 7.6.7).
const contentSecurityPolicy = "default-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// strictTransportSecurity is sent only when the deployment is HTTPS-only
// (CookieSecure, the same condition that gates the cookie's Secure attribute):
// on the plain-HTTP development setup it would be meaningless at best and
// would pin the browser to a scheme the dev instance does not speak at worst.
//
// Deliberately without includeSubDomains. The panel commonly lives on a
// subdomain of a domain used for other things, but it may also sit at the
// apex — and there the directive would force HTTPS on every unrelated
// subdomain of that domain for a year, which is not SelfPost's call to make.
const strictTransportSecurity = "max-age=31536000"

// secure wraps the whole router with the panel's two transport-level
// defences: the security response headers, and an origin check on every
// state-changing request.
//
// It sits outside the authentication middleware on purpose, so that POST
// /login and POST /setup/{token} are covered too.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		// Equivalent to the CSP's frame-ancestors directive, kept for browsers
		// that predate it. The two never disagree: both mean "never framed".
		h.Set("X-Frame-Options", "DENY")
		// The panel's behaviour never depends on the referrer, so the strictest
		// value is free: panel URLs carry domain names and record identifiers
		// that need not leak to anything the administrator clicks through to.
		h.Set("Referrer-Policy", "no-referrer")
		if s.cfg.CookieSecure {
			h.Set("Strict-Transport-Security", strictTransportSecurity)
		}

		if !originAllowed(r) {
			// Log both sides of the comparison. The check depends on r.Host
			// being the name the browser actually used, which every reverse
			// proxy fragment this project ships preserves (Apache
			// ProxyPreserveHost On, nginx proxy_set_header Host $host, Caddy
			// and Traefik by default) — but a proxy that rewrites Host turns
			// every form submission into a 403, and without these two values
			// side by side the symptom just looks like "the panel stopped
			// saving anything".
			logf("panel: rejected %s %s: cross-origin request (Sec-Fetch-Site=%q Origin=%q Host=%q); "+
				"if this was a legitimate request, check that the reverse proxy passes the original Host header through",
				r.Method, r.URL.Path, r.Header.Get("Sec-Fetch-Site"), r.Header.Get("Origin"), r.Host)
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// originAllowed reports whether a state-changing request came from the panel's
// own origin. This is what the session cookie's
// SameSite=Lax attribute cannot do on its own: SameSite is judged per *site*
// (registrable domain), so a neighbouring subdomain — a CMS on the same
// domain, a forgotten staging host — counts as same-site and its forged POST
// would arrive with the session cookie attached. Sec-Fetch-Site and Origin are
// judged per *origin*, so they tell that neighbour apart from the panel.
//
// Read-only methods are exempt: every GET route in the panel is a read (the
// delete confirmation at GET /domains/{id}/delete only renders a form), so
// there is nothing for a cross-origin GET to change.
func originAllowed(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}

	// The browser's own account of where the request came from. It cannot be
	// set by page script (a forbidden header name), so when it is present it
	// is the answer: anything but same-origin — including "none", a
	// navigation with no initiator — is refused.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}

	// Older browsers send only Origin. Compare hosts, not full origins: the
	// panel speaks plain HTTP behind the proxy and does not know its own
	// external scheme, while Origin will say https.
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Neither header. A client this old cannot be checked at all; it is
		// let through as the risk consciously accepted (single-admin panel,
		// the administrator picks the browser). Turning this return into
		// false is the whole of the stricter policy.
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		// Unparseable, or the opaque "null" origin (sandboxed iframe,
		// redirected cross-origin POST) — not this panel either way.
		return false
	}
	return u.Host == r.Host
}
