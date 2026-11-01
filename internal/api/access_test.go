package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// users returns each of a's users as "login role".
func accessUsers(t *testing.T, rec *httptest.ResponseRecorder) (api.WhoCanView, []string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %q, want 200", rec.Code, rec.Body)
	}
	var a api.Access
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("decode %q: %v", rec.Body, err)
	}
	users := make([]string, len(a.Users))
	for i, u := range a.Users {
		if u.Since.IsZero() {
			t.Errorf("user %s has no since", u.Login)
		}
		users[i] = u.Login + " " + string(u.Role)
	}
	return a.WhoCanView, users
}

func TestAccess(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))
	check := func(rec *httptest.ResponseRecorder, wantWho api.WhoCanView, want ...string) {
		t.Helper()
		who, users := accessUsers(t, rec)
		if who != wantWho || strings.Join(users, ", ") != strings.Join(want, ", ") {
			t.Errorf("access = %s, %q; want %s, %q", who, users, wantWho, want)
		}
	}
	me := func() api.Me { return getMe(t, h, newRequest(t, http.MethodGet, "/api/me", tabletIP)) }

	check(serve(h, newRequest(t, http.MethodGet, "/api/access", laptopIP)),
		api.WhoCanViewListed, alice+" admin", bob+" viewer")

	// Changes apply to carol's next request.
	check(send(t, h, http.MethodPut, "/api/access/users/"+carol, laptopIP, `{"role": "viewer"}`),
		api.WhoCanViewListed, alice+" admin", bob+" viewer", carol+" viewer")
	if got := me(); got.Role != api.RoleViewer {
		t.Errorf("carol after being made a viewer = %+v", got)
	}
	check(send(t, h, http.MethodPut, "/api/access/users/"+carol, laptopIP, `{"role": "admin"}`),
		api.WhoCanViewListed, alice+" admin", bob+" viewer", carol+" admin")
	if got := me(); got.Role != api.RoleAdmin {
		t.Errorf("carol after being made an admin = %+v", got)
	}
	check(send(t, h, http.MethodDelete, "/api/access/users/"+carol, laptopIP, ""),
		api.WhoCanViewListed, alice+" admin", bob+" viewer")
	if got := me(); got.Role != "" {
		t.Errorf("carol after being removed = %+v", got)
	}
	check(send(t, h, http.MethodDelete, "/api/access/users/"+carol, laptopIP, ""),
		api.WhoCanViewListed, alice+" admin", bob+" viewer")

	check(send(t, h, http.MethodPatch, "/api/access", laptopIP, `{"whoCanView": "tailnet"}`),
		api.WhoCanViewTailnet, alice+" admin", bob+" viewer")
	if got := me(); got.Role != api.RoleViewer {
		t.Errorf("carol when anyone on the tailnet can view = %+v", got)
	}
	check(send(t, h, http.MethodPatch, "/api/access", laptopIP, `{}`),
		api.WhoCanViewTailnet, alice+" admin", bob+" viewer")
}

func TestAccess_errors(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))
	tests := []struct {
		name, method, target, body string
		want                       int
	}{
		{"unknown role", http.MethodPut, "/api/access/users/" + carol, `{"role": "owner"}`, http.StatusBadRequest},
		{"no role", http.MethodPut, "/api/access/users/" + carol, `{}`, http.StatusBadRequest},
		{"unknown field", http.MethodPut, "/api/access/users/" + carol, `{"role": "viewer", "admin": true}`, http.StatusBadRequest},
		{"two values", http.MethodPut, "/api/access/users/" + carol, `{"role": "viewer"} {}`, http.StatusBadRequest},
		{"not JSON", http.MethodPut, "/api/access/users/" + carol, `role=viewer`, http.StatusBadRequest},
		{"bad login", http.MethodPut, "/api/access/users/carol%20smith", `{"role": "viewer"}`, http.StatusBadRequest},
		{"bad login to remove", http.MethodDelete, "/api/access/users/carol%20smith", ``, http.StatusBadRequest},
		{"unknown who can view", http.MethodPatch, "/api/access", `{"whoCanView": "everyone"}`, http.StatusBadRequest},
		// Valid JSON, so only the size limit can refuse it.
		{"too big", http.MethodPatch, "/api/access", `{"whoCanView": "tailnet"` + strings.Repeat(" ", 2<<20) + `}`, http.StatusBadRequest},
		// Roost always keeps an admin.
		{"last admin to viewer", http.MethodPut, "/api/access/users/" + alice, `{"role": "viewer"}`, http.StatusConflict},
		{"last admin removed", http.MethodDelete, "/api/access/users/Alice@Example.com", ``, http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := send(t, h, tt.method, tt.target, laptopIP, tt.body)
			var body api.ErrorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); rec.Code != tt.want || err != nil || body.Error == "" {
				t.Errorf("%s %s %s = %d %q, want %d and an error", tt.method, tt.target, tt.body, rec.Code, rec.Body, tt.want)
			}
		})
	}

	// None of that changed anything.
	_, users := accessUsers(t, serve(h, newRequest(t, http.MethodGet, "/api/access", laptopIP)))
	if got := strings.Join(users, ", "); got != alice+" admin, "+bob+" viewer" {
		t.Errorf("users after failed changes = %s", got)
	}
}

// Headscale names a user by email if they have one and otherwise by name, so
// a login doesn't always have an @, and an OIDC identity can have slashes.
func TestAccess_loginsWithoutAnAtSign(t *testing.T) {
	t.Parallel()
	const dave, erin = "dave", "https://idp.example.com/users/7"
	daveIP, erinIP := netip.MustParseAddr("100.64.0.6"), netip.MustParseAddr("100.64.0.7")
	tn := testTailnet()
	st, err := tn.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	st.Peers = append(st.Peers,
		tailnet.Device{ID: "dave", DNSName: "dave.example.ts.net", IPs: []netip.Addr{daveIP}, Owner: dave, Online: true},
		tailnet.Device{ID: "erin", DNSName: "erin.example.ts.net", IPs: []netip.Addr{erinIP}, Owner: erin, Online: true})
	tn.SetStatus(*st)
	h := api.New(withTailnet(t, tn, api.Options{}))
	me := func(from netip.Addr) api.Me { return getMe(t, h, newRequest(t, http.MethodGet, "/api/me", from)) }

	if got := me(daveIP); got.Login != dave || got.Role != "" {
		t.Errorf("dave before a role = %+v", got)
	}
	for login, role := range map[string]string{dave: "admin", erin: "viewer"} {
		target := "/api/access/users/" + url.PathEscape(login)
		if rec := send(t, h, http.MethodPut, target, laptopIP, `{"role": "`+role+`"}`); rec.Code != http.StatusOK {
			t.Fatalf("PUT %s = %d %q, want 200", target, rec.Code, rec.Body)
		}
	}
	if got := me(daveIP); got.Role != api.RoleAdmin {
		t.Errorf("dave after being made an admin = %+v", got)
	}
	if got := me(erinIP); got.Login != erin || got.Role != api.RoleViewer {
		t.Errorf("erin after being made a viewer = %+v", got)
	}
	target := "/api/access/users/" + url.PathEscape(erin)
	if rec := send(t, h, http.MethodDelete, target, laptopIP, ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE %s = %d %q, want 200", target, rec.Code, rec.Body)
	}
	if got := me(erinIP); got.Role != "" {
		t.Errorf("erin after being removed = %+v", got)
	}
}
