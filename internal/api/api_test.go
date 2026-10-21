package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/manasseh-fikadu/roost/internal/api"
)

func TestNew_health(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{Version: "v1.2.3"}))

	// Health doesn't need anyone to be signed in.
	rec := serve(h, newRequest(t, http.MethodGet, "/api/health", strangerIP))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got api.Health
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body, err)
	}
	if want := (api.Health{Status: "ok", Version: "v1.2.3"}); got != want {
		t.Errorf("body = %+v, want %+v", got, want)
	}
}

func TestNew_unknownAPIPath(t *testing.T) {
	t.Parallel()
	// With a UI present, unknown API paths must still be JSON errors, not the app.
	h := api.New(withTailnet(t, testTailnet(), api.Options{UI: fstest.MapFS{"200.html": {Data: []byte("app")}}}))

	for _, target := range []string{"/api/", "/api/nope", "/api/health/extra"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			rec := serve(h, newRequest(t, http.MethodGet, target, laptopIP))

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			var got api.ErrorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode body %q: %v", rec.Body, err)
			}
			if got.Error != "not found" {
				t.Errorf("error = %q, want %q", got.Error, "not found")
			}
		})
	}
}
