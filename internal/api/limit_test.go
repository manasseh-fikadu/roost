package api_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/manasseh-fikadu/roost/internal/api"
)

// A machine that tries codes or secrets in a loop is slowed down before
// Roost looks at what it sent.
func TestRateLimit_enroll(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))

	for i := range 20 {
		if code := sendAs(t, h, http.MethodPost, "/api/agent/enroll", laptopIP, "not a code", "").Code; code != http.StatusUnauthorized {
			t.Fatalf("try %d = %d, want a refusal of the code", i+1, code)
		}
	}
	rec := sendAs(t, h, http.MethodPost, "/api/agent/enroll", laptopIP, "not a code", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the 21st try = %d %s, want 429", rec.Code, rec.Body)
	}
	if secs, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || secs < 1 || secs > 4 {
		t.Errorf("Retry-After = %q, want a few seconds", rec.Header().Get("Retry-After"))
	}

	// A good code from the machine that tried too often waits as well, and one
	// machine's tries don't count against another's.
	code := newCode(t, st, time.Now())
	if rec := sendAs(t, h, http.MethodPost, "/api/agent/enroll", laptopIP, code, ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("a good code from the same machine = %d, want 429", rec.Code)
	}
	if rec := sendAs(t, h, http.MethodPost, "/api/agent/enroll", serverIP, code, ""); rec.Code != http.StatusOK {
		t.Errorf("a good code from another machine = %d %s, want it to enroll", rec.Code, rec.Body)
	}
}

func TestRateLimit_installScript(t *testing.T) {
	t.Parallel()
	h := api.New(withStore(t, testTailnet(), testStore(t), api.Options{}))
	var body string
	for range 21 {
		rec := serve(h, newRequest(t, http.MethodGet, "/api/agent/install/nope", laptopIP))
		body = rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want a script even for a refusal", rec.Code)
		}
	}
	if want := "too many install commands"; !strings.Contains(body, want) {
		t.Errorf("the 21st script = %q, want it to say %q", body, want)
	}
}

func TestRateLimit_heartbeat(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))
	secret := enrolled(t, st, "laptop")

	var codes []int
	for range 25 {
		codes = append(codes, sendBeat(t, h, laptopIP, secret, laptopBeat).Code)
	}
	var ok, limited int
	for _, c := range codes {
		switch c {
		case http.StatusNoContent:
			ok++
		case http.StatusTooManyRequests:
			limited++
		}
	}
	// An agent that reports every 15 seconds is nowhere near it.
	if ok < 20 || limited == 0 || ok+limited != 25 {
		t.Errorf("25 heartbeats at once gave %d ok and %d refused (%v), want a burst of about 20 and then 429s", ok, limited, codes)
	}
	if rec := sendBeat(t, h, phoneIP, enrolled(t, st, "phone"), laptopBeat); rec.Code == http.StatusTooManyRequests {
		t.Errorf("another machine's heartbeat was refused because the laptop's were")
	}
}
