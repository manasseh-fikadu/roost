package api_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/api"
)

type envVar struct {
	Key   string `json:"key"`
	Value string `json:"value" bield:"secret"`
}

type Labels struct {
	Token string `json:"token" bield:"secret"`
}

type process struct {
	PID     int     `json:"pid"`
	Command *string `json:"command" bield:"secret"`
}

type container struct {
	Name      string            `json:"name"`
	Env       []envVar          `json:"env"`
	ByKey     map[string]envVar `json:"byKey"`
	Pair      [2]envVar         `json:"pair"`
	Processes []*process        `json:"processes"`
	Extra     any               `json:"extra"`
	Parent    *container        `json:"parent"`
	Labels                      // embedded, so JSON flattens it
	note      string
}

func newContainer() container {
	cmd := "server hunter2"
	return container{
		Name:      "web",
		Env:       []envVar{{"PATH", "/bin"}, {"API_KEY", "hunter2"}},
		ByKey:     map[string]envVar{"API_KEY": {"API_KEY", "hunter2"}},
		Pair:      [2]envVar{{"A", "hunter2"}, {"B", "hunter2"}},
		Processes: []*process{{PID: 1, Command: &cmd}, nil},
		Extra:     envVar{"EXTRA", "hunter2"},
		Parent:    &container{Name: "parent", Env: []envVar{{"P", "hunter2"}}},
		Token:     "hunter2",
		note:      "kept",
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()
	orig := newContainer()

	got, ok := api.Redact(orig).(container)
	if !ok {
		t.Fatalf("Redact() returned %T, want container", api.Redact(orig))
	}

	want := container{
		Name:      "web",
		Env:       []envVar{{Key: "PATH"}, {Key: "API_KEY"}},
		ByKey:     map[string]envVar{"API_KEY": {Key: "API_KEY"}},
		Pair:      [2]envVar{{Key: "A"}, {Key: "B"}},
		Processes: []*process{{PID: 1}, nil},
		Extra:     envVar{Key: "EXTRA"},
		Parent:    &container{Name: "parent", Env: []envVar{{Key: "P"}}},
		note:      "kept",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Redact() = %+v\nwant %+v", got, want)
	}
	// It copies what it changes, and leaves the original as it was.
	if !reflect.DeepEqual(orig, newContainer()) {
		t.Errorf("Redact() changed its argument: %+v", orig)
	}
	body, err := json.Marshal(got)
	if err != nil || strings.Contains(string(body), "hunter2") {
		t.Errorf("redacted JSON = %s, %v; want no secrets", body, err)
	}
}

func TestRedact_noSecrets(t *testing.T) {
	t.Parallel()
	type device struct {
		Name string
		IPs  []string
	}
	d := &device{Name: "laptop", IPs: []string{"100.64.0.2"}}
	tests := []struct {
		name string
		v    any
	}{
		{"nil", nil},
		{"string", "hunter2"},
		{"pointer without secrets", d},
		{"slice without secrets", []device{*d}},
		{"nil pointer", (*container)(nil)},
		{"nil slice", []envVar(nil)},
		{"nil map", map[string]envVar(nil)},
		{"nil interface field", container{Name: "web"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := api.Redact(tt.v); !reflect.DeepEqual(got, tt.v) {
				t.Errorf("Redact(%#v) = %#v, want it unchanged", tt.v, got)
			}
		})
	}
	// A value that can't hold a secret isn't copied.
	if got := api.Redact(d); got != any(d) {
		t.Errorf("Redact(%p) = %p, want the same pointer", d, got)
	}
}

// A struct that refers to itself has to be walked without looping forever.
func TestRedact_recursive(t *testing.T) {
	t.Parallel()
	type node struct {
		Secret string `bield:"secret"`
		Next   *node
	}
	list := &node{Secret: "a", Next: &node{Secret: "b", Next: &node{Secret: "c"}}}
	got, _ := api.Redact(list).(*node)
	if want := (&node{Next: &node{Next: &node{}}}); !reflect.DeepEqual(got, want) {
		t.Errorf("Redact() = %+v, want every Secret empty", got)
	}
	if list.Next.Next.Secret != "c" {
		t.Error("Redact() changed its argument")
	}
}

type hidden struct {
	Token string `json:"token" bield:"secret"`
}

// JSON encodes the fields of an embedded struct even if its type isn't
// exported, but reflection can't clear them, so that's refused.
func TestRedact_unexportedEmbedded(t *testing.T) {
	t.Parallel()
	type leaky struct{ hidden }
	defer func() {
		if recover() == nil {
			t.Error("Redact() of a secret in an unexported embedded struct didn't panic")
		}
	}()
	api.Redact(leaky{hidden{"hunter2"}})
}

// writeJSON leaves secrets out of every answer to someone who isn't an admin.
func TestWriteJSON_redacts(t *testing.T) {
	t.Parallel()
	h := api.ServeValue(withTailnet(t, testTailnet(), api.Options{}), newContainer())

	admin := serve(h, newRequest(t, http.MethodGet, "/value", laptopIP))
	if admin.Code != http.StatusOK || strings.Count(admin.Body.String(), "hunter2") != 8 {
		t.Errorf("GET /value as an admin = %d %s, want every secret", admin.Code, admin.Body)
	}
	viewer := serve(h, newRequest(t, http.MethodGet, "/value", phoneIP))
	if viewer.Code != http.StatusOK || strings.Contains(viewer.Body.String(), "hunter2") ||
		!strings.Contains(viewer.Body.String(), `"API_KEY"`) {
		t.Errorf("GET /value as a viewer = %d %s, want everything but the secrets", viewer.Code, viewer.Body)
	}
}
