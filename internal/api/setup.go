package api

import (
	"bytes"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/skip2/go-qrcode"

	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// SetupOptions configures Setup.
type SetupOptions struct {
	// Tailnet is the embedded node to log in.
	Tailnet tailnet.Tailnet
	// AppURL returns the app's URL on the tailnet once Roost serves it there,
	// or "" until then.
	AppURL func() string
	// Logger receives errors that can't be shown on the page. Nil discards
	// them.
	Logger *slog.Logger
}

// Setup returns the handler for the first-run page, which embedded mode
// serves on a local address. Until Roost is on the tailnet, the page shows the
// Tailscale login link and a QR code of it; after that, a link to Roost on the
// tailnet. It serves nothing else, so the local address can't reach the app.
//
// Whoever opens the login link decides which tailnet Roost joins, so the page
// only answers requests addressed to localhost or an IP address. A web page
// can't read it by pointing its own domain at 127.0.0.1 (DNS rebinding).
func Setup(opts SetupOptions) http.Handler {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; "+
			"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")

		if !localHost(r.Host) {
			http.Error(w, "Open this page at http://localhost or by IP address.", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		page := setupPage{AppURL: opts.AppURL()}
		st, err := opts.Tailnet.Status(r.Context())
		if err != nil {
			page.Error = err.Error()
		} else {
			page.State, page.LoginURL = st.State, st.LoginURL
		}
		if page.LoginURL != "" {
			if page.QR, err = qrSVG(page.LoginURL); err != nil {
				log.Error("make the login QR code", "error", err)
			}
		}

		var buf bytes.Buffer
		if err := setupTemplate.Execute(&buf, page); err != nil {
			log.Error("render the setup page", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		_, _ = buf.WriteTo(w)
	})
}

// localHost reports whether a request's Host header names this machine in a
// way that a web page can't take over: localhost, a name under .localhost,
// which browsers always resolve to loopback, or an IP address.
func localHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	_, err = netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil
}

type setupPage struct {
	State    string
	LoginURL string
	QR       template.HTML
	AppURL   string
	Error    string
}

// Running reports whether Roost is on the tailnet and serving.
func (p setupPage) Running() bool { return p.State == tailnet.StateRunning && p.AppURL != "" }

// qrSVG draws text as a QR code in SVG.
func qrSVG(text string) (template.HTML, error) {
	q, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bits := q.Bitmap() // includes the quiet zone around the code
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" `+
		`role="img" aria-label="QR code of the login link"><rect width="100%%" height="100%%" fill="#fff"/><path d="`,
		len(bits), len(bits))
	for y, row := range bits {
		for x, on := range row {
			if on {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x, y)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	//nolint:gosec // G203: the markup is built from numbers only.
	return template.HTML(b.String()), nil
}

var setupTemplate = template.Must(template.New("setup").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{- if not .Running}}
<meta http-equiv="refresh" content="3">
{{- end}}
<title>Set up Roost</title>
<style>
:root { color-scheme: light dark; font-family: system-ui, sans-serif; line-height: 1.5; }
body { margin: 0; min-height: 100dvh; display: grid; place-items: center; background: Canvas; color: CanvasText; }
main { max-width: 26rem; padding: 2rem 1.5rem; }
h1 { font-size: 1.25rem; font-weight: 600; margin: 0 0 .5rem; }
p { margin: 0 0 1rem; }
.muted { opacity: .7; font-size: .875rem; }
.button { display: inline-block; padding: .6rem 1rem; border-radius: .5rem; background: #1c1917; color: #fff; text-decoration: none; font-weight: 600; }
@media (prefers-color-scheme: dark) { .button { background: #fafaf9; color: #1c1917; } }
.qr { width: 12rem; margin: 1.5rem 0; }
.qr svg { display: block; width: 100%; height: auto; border-radius: .5rem; }
code { font-size: .875em; }
</style>
</head>
<body>
<main>
{{- if .Error}}
<h1>Roost can't reach Tailscale</h1>
<p>{{.Error}}</p>
<p class="muted">This page tries again every few seconds.</p>
{{- else if .Running}}
<h1>Roost is on your tailnet</h1>
<p>Open it from any device on your tailnet.</p>
<p><a class="button" href="{{.AppURL}}">Open Roost</a></p>
<p class="muted">{{.AppURL}}</p>
{{- else if .LoginURL}}
<h1>Add Roost to your tailnet</h1>
<p>Log in to Tailscale to add Roost to your tailnet as a new machine.</p>
<p><a class="button" href="{{.LoginURL}}" target="_blank" rel="noopener noreferrer">Log in to Tailscale</a></p>
{{- if .QR}}
<div class="qr">{{.QR}}</div>
<p class="muted">Or scan the code to log in on your phone.</p>
{{- end}}
<p class="muted">To skip this step, restart Roost with <code>TS_AUTHKEY</code> set to an auth key.</p>
{{- else}}
<h1>Connecting to Tailscale…</h1>
<p class="muted">{{with .State}}Tailscale is {{.}}. {{end}}This page updates by itself.</p>
{{- end}}
</main>
</body>
</html>
`))
