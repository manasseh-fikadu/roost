//go:build ui

package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/config"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
	"github.com/manasseh-fikadu/bield/internal/tailnet/tailnettest"
)

// TestServe_ui checks that a build with the ui tag serves the web app on the
// tailnet. It needs web/build, so CI runs it after `make build`.
func TestServe_ui(t *testing.T) {
	t.Parallel()
	tn := fakeTailnet(tailnet.StateRunning)
	log := startServe(t, testConfig(t, config.Embedded), tn)
	log.waitFor(t, "bield is running")

	// An app route gets the app's page, which loads the bundled JS.
	code, body := get(t, tn.Client(laptopIP), tailnettest.URL+"/devices")

	if code != http.StatusOK || !strings.Contains(body, "_app/immutable/") {
		t.Errorf("GET /devices on the tailnet = %d, want 200 and the app's page:\n%s", code, body)
	}
	if strings.Contains(log.String(), "built without the web UI") {
		t.Errorf("runServe() found no web UI in a ui build; log:\n%s", log)
	}
}
