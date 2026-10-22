package usage_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/usage"
)

// sampleAt returns a sample from an agent at a time, in which a web
// container uses cpu percent and mem bytes, and so does its one process.
func sampleAt(at time.Time, cpu float64, mem uint64) usage.Sample {
	return usage.Sample{
		At: at, Source: usage.SourceAgent, Cores: 4,
		Groups: []usage.Group{
			{Key: "container:web", Kind: usage.KindContainer, Name: "web", CPU: cpu, Mem: mem, Procs: 1, Top: []string{"nginx"}},
		},
		Procs: []usage.Proc{{PID: 7, Name: "nginx", User: "www", Cmd: strings.Repeat("x", 300), CPU: cpu, Mem: mem, Group: "container:web"}},
		Total: &usage.Totals{CPU: cpu, Mem: mem, Procs: 1},
	}
}

func TestRecorder(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := openStore(t)
		// Let the prune that Open starts finish, so SQLite doesn't wait in
		// fake time.
		synctest.Wait()
		rec := usage.NewRecorder(st, nil)
		start := time.Now()

		if _, _, ok := rec.Latest("laptop"); ok {
			t.Error("Latest() before any sample found one")
		}
		// A sample every 15 seconds: 0, 15, 30 and 45 make a minute, and
		// the one at 60 starts the next.
		for i, cpu := range []float64{10, 20, 30, 40, 50} {
			rec.Record(t.Context(), "laptop", sampleAt(start.Add(time.Duration(i)*15*time.Second), cpu, uint64(1000*(i+1))))
		}
		latest, interval, ok := rec.Latest("laptop")
		if !ok || latest.Groups[0].CPU != 50 || interval != 15*time.Second {
			t.Errorf("Latest() = %+v, %v, %v, want the last sample, every 15s", latest, interval, ok)
		}

		got, ok, err := rec.At(t.Context(), "laptop", start.Add(40*time.Second), time.Minute)
		if err != nil || !ok {
			t.Fatalf("At() = %v, %v", ok, err)
		}
		want := sampleAt(start.Add(45*time.Second), 25, 4000)
		want.Procs[0].Cmd = strings.Repeat("x", 200)
		// The total is the last sample's.
		want.Total.CPU = 40
		if !reflect.DeepEqual(got, want) {
			t.Errorf("At() =\n%+v\nwant the minute's average CPU and its last memory:\n%+v", got, want)
		}

		// The next minute stops after one sample. Run stores it after a while.
		ctx, cancel := context.WithCancel(t.Context())
		go rec.Run(ctx)
		time.Sleep(4 * time.Minute)
		synctest.Wait()
		cancel()
		if got, ok, err := rec.At(t.Context(), "laptop", start.Add(time.Minute), time.Second); err != nil || !ok || got.Groups[0].CPU != 50 {
			t.Errorf("At() the second minute = %+v, %v, %v, want the sample at 1m", got, ok, err)
		}

		rec.Forget("laptop")
		if _, _, ok := rec.Latest("laptop"); ok {
			t.Error("Latest() after Forget() found a sample")
		}
	})
}

func TestRecorder_History(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := openStore(t)
		synctest.Wait()
		rec := usage.NewRecorder(st, nil)
		start := time.Now()
		group := func(key string, cpu float64, mem uint64) usage.Group {
			kind, name := usage.SplitKey(key)
			return usage.Group{Key: key, Kind: kind, Name: name, CPU: cpu, Mem: mem}
		}
		// Three minutes of one sample each. A sample a minute stores the
		// one before.
		for i, groups := range [][]usage.Group{
			{group("service:db", 30, 1000), group("service:web", 10, 5000)},
			{group("service:db", 60, 3000)},
			{group("service:db", 90, 2000), group("container:cache", 1, 100)},
			{},
		} {
			rec.Record(t.Context(), "server", usage.Sample{At: start.Add(time.Duration(i) * time.Minute), Source: usage.SourceHub, Groups: groups})
		}

		h, err := rec.History(t.Context(), "server", start, 2*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if h.Minutes != 3 {
			t.Errorf("Minutes = %d, want 3", h.Minutes)
		}
		wantConsumers := []usage.Consumer{
			{Key: "service:db", Kind: usage.KindService, Name: "db", CPUAvg: 60, CPUPeak: 90, MemAvg: 2000, MemPeak: 3000},
			{Key: "service:web", Kind: usage.KindService, Name: "web", CPUAvg: 3.33, CPUPeak: 10, MemAvg: 1667, MemPeak: 5000},
			{Key: "container:cache", Kind: usage.KindContainer, Name: "cache", CPUAvg: 0.33, CPUPeak: 1, MemAvg: 33, MemPeak: 100},
		}
		if !reflect.DeepEqual(h.Consumers, wantConsumers) {
			t.Errorf("Consumers =\n%+v\nwant\n%+v", h.Consumers, wantConsumers)
		}
		// Steps start at multiples of two minutes, and synctest starts at
		// midnight, so the minutes fall in two steps: 0 and 1, then 2.
		points := func(s []usage.Series) map[string][]float64 {
			out := map[string][]float64{}
			for _, x := range s {
				for _, p := range x.Points {
					out[x.Key] = append(out[x.Key], p.V)
				}
			}
			return out
		}
		if got, want := points(h.CPU), map[string][]float64{"service:db": {45, 90}, "service:web": {5, 0}, "container:cache": {0, 1}}; !reflect.DeepEqual(got, want) {
			t.Errorf("CPU series = %v, want %v", got, want)
		}
		if got, want := points(h.Mem), map[string][]float64{"service:db": {2000, 2000}, "service:web": {2500, 0}, "container:cache": {0, 100}}; !reflect.DeepEqual(got, want) {
			t.Errorf("memory series = %v, want %v", got, want)
		}
		if len(h.CPU) > 0 && !h.CPU[0].Points[1].At.Equal(start.Add(2*time.Minute)) {
			t.Errorf("second step starts at %v, want %v", h.CPU[0].Points[1].At, start.Add(2*time.Minute))
		}

		if _, err := rec.History(t.Context(), "server", start, 0); err == nil {
			t.Error("History() with a zero step didn't fail")
		}
	})
}
