package store_test

import (
	"path/filepath"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

func TestStore_DeviceSettings(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	no := false
	set := func(id string, ds store.DeviceSettings) {
		t.Helper()
		if err := s.SetDeviceSettings(t.Context(), id, ds); err != nil {
			t.Fatalf("SetDeviceSettings(%s) error = %v", id, err)
		}
	}
	get := func() map[string]store.DeviceSettings {
		t.Helper()
		got, err := s.DeviceSettings(t.Context())
		if err != nil {
			t.Fatalf("DeviceSettings() error = %v", err)
		}
		return got
	}

	if got := get(); len(got) != 0 {
		t.Errorf("settings of a new database = %v, want none", got)
	}
	phone := store.DeviceSettings{DisplayName: "Pixel", Kind: "phone", AlertOffline: &no, Notes: "In the drawer"}
	set("phone", phone)
	set("laptop", store.DeviceSettings{DisplayName: "Work laptop"})
	set("laptop", store.DeviceSettings{Notes: "Replaced"})
	// A device can have some thresholds of its own and not others.
	nas := store.DeviceSettings{Thresholds: store.ThresholdOverrides{DiskWarn: new(97.0), DiskCritical: new(99.0), OfflineMinutes: new(60)}}
	set("nas", nas)
	want := map[string]store.DeviceSettings{"phone": phone, "laptop": {Notes: "Replaced"}, "nas": nas}
	if got := get(); !reflect.DeepEqual(got, want) {
		t.Errorf("settings = %+v, want %+v", got, want)
	}
	nas.Thresholds = store.ThresholdOverrides{CPUWarn: new(100.0), MemoryWarn: new(99.5)}
	set("nas", nas)
	if got := get()["nas"]; !reflect.DeepEqual(got, nas) {
		t.Errorf("thresholds after changing them = %+v, want %+v", got, nas)
	}

	// The defaults need no row.
	set("laptop", store.DeviceSettings{})
	set("nas", store.DeviceSettings{})
	if got := firstColumn(t, s.DB(), "SELECT device_id FROM device_settings"); !reflect.DeepEqual(got, []string{"phone"}) {
		t.Errorf("rows after resetting the laptop and the NAS = %q, want only the phone's", got)
	}
}

func TestThresholdOverrides_Or(t *testing.T) {
	t.Parallel()
	own := store.ThresholdOverrides{DiskWarn: new(97.0), OfflineMinutes: new(60)}
	other := store.ThresholdOverrides{DiskWarn: new(50.0), DiskCritical: new(99.0), CPUWarn: new(100.0), OfflineMinutes: new(5)}
	want := store.ThresholdOverrides{DiskWarn: new(97.0), DiskCritical: new(99.0), CPUWarn: new(100.0), OfflineMinutes: new(60)}
	if got := own.Or(other); !reflect.DeepEqual(got, want) {
		t.Errorf("Or() = %+v, want %+v", got, want)
	}
	if got := (store.ThresholdOverrides{}).Or(store.ThresholdOverrides{}); got != (store.ThresholdOverrides{}) {
		t.Errorf("Or() of nothing = %+v", got)
	}
}

func TestStore_RTTHistory(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	start := time.UnixMilli(1_800_000_000_000).Truncate(5 * time.Minute)
	reply := func(at time.Duration, rtt time.Duration) store.Ping {
		return store.Ping{DeviceID: "laptop", At: start.Add(at), Replied: true, RTT: rtt}
	}
	pings := []store.Ping{
		reply(-time.Minute, time.Second), // before the history starts
		reply(0, 10*time.Millisecond),
		reply(time.Minute, 20*time.Millisecond),
		{DeviceID: "laptop", At: start.Add(2 * time.Minute)}, // lost
		{DeviceID: "phone", At: start.Add(3 * time.Minute), Replied: true, RTT: time.Second},
		// Nothing from 5 to 10 minutes, then only lost pings.
		{DeviceID: "laptop", At: start.Add(10 * time.Minute)},
		reply(15*time.Minute+time.Second, 1500*time.Microsecond),
	}
	if err := s.AddPings(t.Context(), pings); err != nil {
		t.Fatalf("AddPings() error = %v", err)
	}

	got, err := s.RTTHistory(t.Context(), "laptop", start, 5*time.Minute)
	if err != nil {
		t.Fatalf("RTTHistory() error = %v", err)
	}
	want := []store.RTTBucket{
		{Start: start, Pings: 3, Replies: 2, RTT: 15 * time.Millisecond},
		{Start: start.Add(10 * time.Minute), Pings: 1},
		{Start: start.Add(15 * time.Minute), Pings: 1, Replies: 1, RTT: 1500 * time.Microsecond},
	}
	if len(got) != len(want) {
		t.Fatalf("RTTHistory() = %+v, want %+v", got, want)
	}
	for i := range want {
		if g := got[i]; !g.Start.Equal(want[i].Start) || g.Pings != want[i].Pings || g.Replies != want[i].Replies || g.RTT != want[i].RTT {
			t.Errorf("bucket %d = %+v, want %+v", i, g, want[i])
		}
	}
	if _, err := s.RTTHistory(t.Context(), "laptop", start, time.Microsecond); err == nil {
		t.Error("RTTHistory() with buckets under a millisecond didn't fail")
	}
}

func TestStore_prunesPingsOnceTheyAreSummedUp(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const week = 7 * 24 * time.Hour
		s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
		synctest.Wait()
		now := time.Now()
		err := s.AddPings(t.Context(), []store.Ping{
			{DeviceID: "expired", At: now.Add(-week - time.Minute)},
			{DeviceID: "kept", At: now.Add(-week + 2*time.Hour)},
		})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(store.PruneInterval)
		synctest.Wait()
		// Raw pings go after two days, once they're in buckets, which last a week.
		if got := firstColumn(t, s.DB(), "SELECT device_id FROM pings"); len(got) != 0 {
			t.Errorf("raw pings after an hour = %q, want none", got)
		}
		if got := firstColumn(t, s.DB(), "SELECT DISTINCT device_id FROM pings_rollup"); !reflect.DeepEqual(got, []string{"kept"}) {
			t.Errorf("ping buckets after an hour = %q, want only the one that's under a week old", got)
		}
	})
}
