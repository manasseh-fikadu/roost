package api

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
)

// securityHeaders puts the headers that every response has on it: what the
// browser must not sniff, send as a referrer or let another site frame or
// read. A handler that sets one of these itself, such as the file routes with
// their own Content-Security-Policy, wins.
//
// Roost opens shells on every machine, so a page on another site that could
// frame the terminal, and click through it, is as bad as a bug in it.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// Roost's own pages may frame its files, such as a PDF. No one else may
		// frame anything, and the web app overrides this to say no one at all.
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Content-Security-Policy", "frame-ancestors 'self'")
		// Another site can't load a response of Roost's, even with no-cors.
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), interest-cohort=()")
		next.ServeHTTP(w, r)
	})
}

// inlineScript matches a <script> that has its code inside it.
var inlineScript = regexp.MustCompile(`(?is)<script(\s[^>]*)?>(.*?)</script>`)

// hasSrc matches the attributes of a script that loads its code from a file.
var hasSrc = regexp.MustCompile(`(?i)(^|\s)src\s*=`)

// uiPolicy works out the Content-Security-Policy of the web app's pages from
// the page itself. SvelteKit starts the app with an inline script whose text
// changes with every build, so the policy allows that script by its hash, and
// no other: nothing in the page can run script that Roost didn't build, and
// no page can load more of it from another site, so a file's text or a
// machine's hostname that reached the page as HTML would still be inert.
//
// Styles are the one place it gives way: components and libraries such as the
// terminal and the editor set style attributes and add rules, which an
// inline-style ban would break. Style can't run code.
//
// Remote images are blocked too, which means a Markdown file on a machine
// can't have the browser fetch a picture from anywhere else just because an
// admin opened it.
func uiPolicy(fsys fs.FS) *policy {
	var hashes []string
	if b, err := fs.ReadFile(fsys, fallbackPage); err == nil {
		for _, m := range inlineScript.FindAllStringSubmatch(string(b), -1) {
			if hasSrc.MatchString(m[1]) {
				continue
			}
			sum := sha256.Sum256([]byte(m[2]))
			hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
		}
	}
	return &policy{base: strings.Join([]string{
		"default-src 'none'",
		strings.Join(append([]string{"script-src 'self'"}, hashes...), " "),
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		// A document's viewer, in a frame of its own, reads fonts as data.
		"font-src 'self' data:",
		"frame-src 'self'",
		"media-src 'self'",
		"manifest-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		// The terminal can't be clickjacked: nothing frames the app.
		"frame-ancestors 'none'",
	}, "; ")}
}

// A policy is a Content-Security-Policy that needs the host it's served on.
type policy struct{ base string }

// plainHost matches a host name or address with its port, which can sit in a
// policy as it is.
var plainHost = regexp.MustCompile(`^[A-Za-z0-9.\-:\[\]]+$`)

// header returns the policy for a page that was asked for at r.Host. The
// page talks to Roost, and its terminal opens a WebSocket, which some
// browsers don't count as 'self'.
func (p *policy) header(r *http.Request) string {
	connect := "connect-src 'self'"
	if plainHost.MatchString(r.Host) {
		connect += " ws://" + r.Host + " wss://" + r.Host
	}
	return p.base + "; " + connect
}

// apply sets the headers of a page of the web app. The page can't be framed
// by anything.
func (p *policy) apply(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", p.header(r))
	w.Header().Set("X-Frame-Options", "DENY")
}
