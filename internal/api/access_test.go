package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/api"
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
		{"bad login", http.MethodPut, "/api/access/users/carol", `{"role": "viewer"}`, http.StatusBadRequest},
		{"bad login to remove", http.MethodDelete, "/api/access/users/carol", ``, http.StatusBadRequest},
		{"unknown who can view", http.MethodPatch, "/api/access", `{"whoCanView": "everyone"}`, http.StatusBadRequest},
		{"too big", http.MethodPatch, "/api/access", `{"whoCanView": "` + strings.Repeat("x", 2<<20) + `"}`, http.StatusBadRequest},
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
