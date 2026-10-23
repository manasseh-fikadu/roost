package agents_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/hostmetrics"
	"github.com/manasseh-fikadu/roost/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func heartbeat(t *testing.T, body string) agents.Heartbeat {
	t.Helper()
	var hb agents.Heartbeat
	if err := json.Unmarshal([]byte(body), &hb); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return hb
}

func TestRegistry_Record(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, body string
		want       agents.Report
	}{
		{
			name: "from a Linux laptop",
			body: `{"v":1,"agentVersion":"1.2.0","hostname":"laptop","os":"linux",
				"osVersion":"Debian GNU/Linux 13 (trixie)","kernel":"6.12.48+deb13-amd64","model":"ThinkPad X1","arch":"x86_64",
				"metrics":{"cpu":12.5,"cores":8,"load":[0.5,0.25,0.1],"memUsed":4000,"memTotal":16000,"swapUsed":0,"swapTotal":2000,
				"diskUsed":100,"diskTotal":500,"netRxTotal":1000,"netTxTotal":2000,"temp":45.5,
				"battery":{"pct":80,"charging":true,"onAc":true},"uptime":3600,"procs":250}}`,
			want: agents.Report{
				Version: "1.2.0",
				Sample: hostmetrics.Sample{
					CPU: new(12.5), Load: &[3]float64{0.5, 0.25, 0.1}, Procs: 250,
					Mem: hostmetrics.Usage{Used: 4000, Total: 16000}, Swap: &hostmetrics.Usage{Total: 2000},
					Disk: &hostmetrics.Usage{Used: 100, Total: 500}, Temp: new(45.5), Uptime: time.Hour,
				},
				Info: hostmetrics.Info{
					Hostname: "laptop", OSVersion: "Debian GNU/Linux 13 (trixie)", Kernel: "6.12.48+deb13-amd64",
					Model: "ThinkPad X1", Arch: "x86_64", Cores: 8,
				},
				Battery: &agents.Battery{Percent: 80, Charging: true, OnAC: true},
			},
		},
		{
			name: "nothing but the format",
			body: `{"v":1}`,
		},
		{
			// The agent sends null for what the machine doesn't say.
			name: "nulls",
			body: `{"v":1,"hostname":null,"metrics":{"cpu":null,"cores":null,"load":[null,null,null],"memUsed":null,"memTotal":null,
				"diskUsed":null,"diskTotal":null,"temp":null,"battery":null,"uptime":null,"procs":null}}`,
		},
		{
			name: "values out of range or of the wrong type",
			body: `{"v":1,"agentVersion":"1.2.0-a-very-long-version-string-indeed","hostname":"two\nlines","model":7,
				"metrics":{"cpu":250,"cores":-2,"load":[1,2],"memUsed":9000,"memTotal":1000,"swapTotal":0,
				"diskUsed":"lots","diskTotal":1e30,"temp":5000,"battery":{"pct":"full"},"uptime":-5,"procs":"many"}}`,
			want: agents.Report{
				Version: "1.2.0-a-very-long-version-string",
				Sample: hostmetrics.Sample{
					CPU:  new(100.0),
					Mem:  hostmetrics.Usage{Used: 1000, Total: 1000},
					Disk: &hostmetrics.Usage{Total: 1 << 60},
				},
				Info: hostmetrics.Info{Hostname: "two lines"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg := agents.NewRegistry(openStore(t), nil)
			now := time.Unix(1_800_000_000, 0)
			got, err := reg.Record(t.Context(), "laptop", heartbeat(t, tt.body), now)
			if err != nil {
				t.Fatalf("Record() error = %v", err)
			}
			want := tt.want
			want.ReceivedAt, want.Sample.At = now, now
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Record() =\n%+v\nwant\n%+v", got, want)
			}
			if r := reg.Reports()["laptop"]; !reflect.DeepEqual(r, got) {
				t.Errorf("Reports()[laptop] = %+v, want what Record returned", r)
			}
		})
	}
}

func TestRegistry_Record_rates(t *testing.T) {
	t.Parallel()
	reg := agents.NewRegistry(openStore(t), nil)
	start := time.Unix(1_800_000_000, 0)
	beat := func(after time.Duration, rx, tx int) agents.Report {
		t.Helper()
		body := []byte(`{"v":1,"metrics":{"cpu":95,"netRxTotal":` + strconv.Itoa(rx) + `,"netTxTotal":` + strconv.Itoa(tx) + `}}`)
		if rx < 0 {
			body = []byte(`{"v":1,"metrics":{"cpu":95}}`)
		}
		var hb agents.Heartbeat
		if err := json.Unmarshal(body, &hb); err != nil {
			t.Fatal(err)
		}
		r, err := reg.Record(t.Context(), "laptop", hb, start.Add(after))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	rates := func(r agents.Report) []float64 {
		if r.Sample.NetRx == nil {
			return nil
		}
		return []float64{*r.Sample.NetRx, *r.Sample.NetTx}
	}

	if r := beat(0, 1000, 500); rates(r) != nil {
		t.Errorf("rates after the first heartbeat = %v, want none", rates(r))
	}
	if r := beat(10*time.Second, 11_000, 2500); !reflect.DeepEqual(rates(r), []float64{1000, 200}) {
		t.Errorf("rates = %v, want [1000 200] bytes a second", rates(r))
	}
	// The machine rebooted, so the counters started again.
	if r := beat(20*time.Second, 50, 50); rates(r) != nil {
		t.Errorf("rates after the counters went back = %v, want none", rates(r))
	}
	// Nothing for a long while.
	if r := beat(time.Hour, 100, 100); rates(r) != nil {
		t.Errorf("rates after an hour's gap = %v, want none", rates(r))
	}
	beat(time.Hour+time.Minute, -1, 0)
	if r := beat(time.Hour+2*time.Minute, 200, 200); rates(r) != nil {
		t.Errorf("rates after a heartbeat without counters = %v, want none", rates(r))
	}

	// Ten minutes of 95% CPU, from a heartbeat a minute.
	var r agents.Report
	for i := range 10 {
		r = beat(2*time.Hour+time.Duration(i)*time.Minute, 0, 0)
	}
	if r.Sample.CPUSustained == nil || *r.Sample.CPUSustained != 95 {
		t.Errorf("CPUSustained = %v, want 95", r.Sample.CPUSustained)
	}
}

func TestRegistry_storesReports(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	reg := agents.NewRegistry(st, nil)
	start := time.Unix(1_800_000_000, 0)
	for i, body := range []string{
		`{"v":1,"agentVersion":"1.0.0","metrics":{"cpu":10}}`,
		// Too soon after the first to be stored.
		`{"v":1,"agentVersion":"1.1.0","metrics":{"cpu":20}}`,
	} {
		if _, err := reg.Record(t.Context(), "laptop", heartbeat(t, body), start.Add(time.Duration(i)*15*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"v":1,"agentVersion":"1.2.0","hostname":"phone-ish","metrics":{"cpu":30,"memUsed":1,"memTotal":2,
		"diskUsed":3,"diskTotal":4,"load":[1,2,3],"netRxTotal":5,"netTxTotal":6,"battery":{"pct":50},"uptime":60,"procs":9}}`
	phone, err := reg.Record(t.Context(), "phone", heartbeat(t, body), start)
	if err != nil {
		t.Fatal(err)
	}

	again := agents.NewRegistry(st, nil)
	if err := again.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := again.Reports()
	if v := got["laptop"].Version; v != "1.0.0" {
		t.Errorf("stored laptop version = %q, want the first heartbeat's, 1.0.0", v)
	}
	want := phone
	// Rates and sustained CPU aren't stored.
	want.Sample.NetRx, want.Sample.NetTx, want.Sample.CPUSustained = nil, nil, nil
	if g := got["phone"]; !g.ReceivedAt.Equal(want.ReceivedAt) || !reflect.DeepEqual(withoutTimes(g), withoutTimes(want)) {
		t.Errorf("stored phone report =\n%+v\nwant\n%+v", g, want)
	}

	if err := again.Forget(t.Context(), "phone"); err != nil {
		t.Errorf("Forget(phone) error = %v", err)
	}
	if err := again.Forget(t.Context(), "phone"); !errors.Is(err, store.ErrNoAgent) {
		t.Errorf("Forget(phone) again: error = %v, want ErrNoAgent", err)
	}
	select {
	case <-again.Changes():
	default:
		t.Error("Forget() didn't signal a change")
	}
	if _, ok := again.Reports()["phone"]; ok {
		t.Error("Reports() still has the phone after Forget()")
	}
}

func TestRegistry_Record_version(t *testing.T) {
	t.Parallel()
	reg := agents.NewRegistry(openStore(t), nil)
	if _, err := reg.Record(t.Context(), "laptop", heartbeat(t, `{"v":2}`), time.Now()); !errors.Is(err, agents.ErrVersion) {
		t.Errorf("Record() of format 2: error = %v, want ErrVersion", err)
	}
	if len(reg.Reports()) != 0 {
		t.Error("Record() kept a heartbeat it refused")
	}
}

func withoutTimes(r agents.Report) agents.Report {
	r.ReceivedAt, r.Sample.At = time.Time{}, time.Time{}
	return r
}
