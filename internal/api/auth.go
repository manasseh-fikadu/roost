package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// CSRFHeader must be set, to anything, on every request that isn't a GET,
// HEAD or OPTIONS. A web page can't send a custom header to another origin
// without Roost's permission, which it never gives, so a page elsewhere can't
// make a signed-in browser change anything.
const CSRFHeader = "X-Roost"

// caller is who sent a request, as Tailscale says, and what Roost lets them do.
type caller struct {
	// who is nil for an anonymous caller.
	who *tailnet.Identity
	// level is anyone, viewers or admins. guard always sets it, so a request
	// that skipped guard has noLevel and can't use any route.
	level  level
	reason string // why an anonymous caller is anonymous: a Reason constant
}

// role returns the caller's role, or "" if they can't view Roost.
func (c caller) role() Role {
	switch c.level {
	case admins:
		return RoleAdmin
	case viewers:
		return RoleViewer
	default:
		return ""
	}
}

type callerKey struct{}

// callerOf returns who sent r. guard put it there.
func callerOf(r *http.Request) caller {
	c, _ := r.Context().Value(callerKey{}).(caller)
	return c
}

// guard checks every request before it reaches a handler, and works out who
// sent it:
//
//   - The Host header must be one of the names of Roost's node, so a page
//     that rebinds its own domain to Roost's address can't talk to Roost as
//     the browser's device.
//   - Requests that change things need CSRFHeader.
//   - WebSockets must come from a page on Roost, since browsers let any page
//     open one.
//
// Then it asks Tailscale who is on the other end of the connection, and the
// store what role they have. No header can change the answer.
func (h *handler) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := h.opts.Snapshot()
		if snap.Status == nil {
			w.Header().Set("Retry-After", "1")
			h.deny(w, r, http.StatusServiceUnavailable, "Roost is starting")
			return
		}
		self := snap.Status.Self
		if !ownHost(self, r.Host) {
			h.deny(w, r, http.StatusMisdirectedRequest, "Roost only answers to its own name on the tailnet")
			return
		}
		switch {
		case isWebSocket(r):
			// Browsers can't add headers to a WebSocket, but they do send Origin.
			if !sameOrigin(r) {
				h.deny(w, r, http.StatusForbidden, "WebSockets must come from Roost's own pages")
				return
			}
		case !safeMethod(r.Method) && r.Header.Get(CSRFHeader) == "":
			h.deny(w, r, http.StatusForbidden, "requests that change things need the "+CSRFHeader+" header")
			return
		}
		c := h.identify(r.Context(), r.RemoteAddr, self.ID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, c)))
	})
}

// identify asks Tailscale who is at remoteAddr. Anyone it can't name is
// anonymous, and so are tagged devices, which belong to no one, and
// connections from Roost's own node. In host mode that node is the machine
// Roost runs on, and any process there would otherwise act as its owner.
// Anonymous callers can only use routes for anyone.
func (h *handler) identify(ctx context.Context, remoteAddr, selfID string) caller {
	who, err := h.opts.WhoIs(ctx, remoteAddr)
	switch {
	case errors.Is(err, tailnet.ErrUnknownPeer):
		return caller{level: anyone, reason: ReasonUnknownDevice}
	case err != nil:
		h.log.Warn("ask Tailscale who sent a request", "remote_addr", remoteAddr, "error", err)
		return caller{level: anyone, reason: ReasonWhoIsFailed}
	case who.NodeID == selfID:
		return caller{level: anyone, reason: ReasonRoostMachine}
	case len(who.Tags) > 0:
		return caller{level: anyone, reason: ReasonTaggedDevice}
	case who.LoginName == "":
		return caller{level: anyone, reason: ReasonNoUser}
	}
	// If Roost can't tell what someone's role is, they have none.
	role, err := h.opts.Store.Role(ctx, who.LoginName)
	if err != nil {
		h.log.Error("look up a caller's role", "login", who.LoginName, "error", err)
	}
	c := caller{who: who, level: anyone}
	switch role {
	case store.Admin:
		c.level = admins
	case store.Viewer:
		c.level = viewers
	}
	return c
}

// deny answers with an error: JSON for the API, and text for anything else.
func (h *handler) deny(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.writeJSON(w, r, status, ErrorBody{Error: msg})
		return
	}
	http.Error(w, msg, status)
}

// ownHost reports whether hostport names Roost's node: its MagicDNS name,
// the short name that MagicDNS also resolves, or one of its tailnet IPs. A
// page can't make the browser send any of these unless it's on Roost.
func ownHost(self tailnet.Device, hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.TrimSuffix(host, ".")
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		for _, own := range self.IPs {
			if ip.Unmap() == own {
				return true
			}
		}
		return false
	}
	if self.DNSName == "" {
		return false
	}
	short, _, _ := strings.Cut(self.DNSName, ".")
	return strings.EqualFold(host, self.DNSName) || strings.EqualFold(host, short)
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// isWebSocket reports whether r opens a WebSocket: an HTTP/1.1 upgrade, or
// an HTTP/2 extended CONNECT (RFC 8441), which net/http shows as a CONNECT
// with a :protocol header. Go turns extended CONNECT off unless GODEBUG
// turns it on, so browsers use HTTP/1.1 for now.
func isWebSocket(r *http.Request) bool {
	if r.Method == http.MethodConnect {
		return strings.EqualFold(r.Header.Get(":protocol"), "websocket")
	}
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// sameOrigin reports whether r's Origin is the page's own origin: the scheme
// it arrived over and its Host, which guard has already checked.
func sameOrigin(r *http.Request) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return origin.Scheme == scheme && strings.EqualFold(origin.Host, r.Host) && origin.Path == ""
}
