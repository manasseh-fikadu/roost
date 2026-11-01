package docker_test

import (
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/docker"
)

func dfEngine(body string) *engine {
	e := newEngine()
	e.answer = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/system/df" {
			return false
		}
		_, _ = w.Write([]byte(body))
		return true
	}
	return e
}

func TestHost_Disk(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, df string
		want     docker.DiskUsage
	}{
		{
			name: "the lists of an older daemon",
			df: `{
				"LayersSize": 900,
				"Images": [
					{"Id": "a", "Size": 500, "SharedSize": 100, "Containers": 1},
					{"Id": "b", "Size": 600, "SharedSize": 200, "Containers": 0},
					{"Id": "c", "Size": 300, "SharedSize": -1, "Containers": 0}
				],
				"Containers": [{"Id": "x", "SizeRw": 10, "State": "running"}, {"Id": "y", "SizeRw": 5, "State": "exited"}],
				"Volumes": [
					{"Name": "used", "UsageData": {"Size": 1000, "RefCount": 2}},
					{"Name": "idle", "UsageData": {"Size": 400, "RefCount": 0}},
					{"Name": "unmeasured", "UsageData": {"Size": -1, "RefCount": -1}}
				],
				"BuildCache": [{"Size": 50}, {"Size": 70}]
			}`,
			want: docker.DiskUsage{
				Images:     docker.DiskGroup{Size: 900, Count: 3, Active: 1, Reclaimable: 400 + 300},
				Containers: docker.DiskGroup{Size: 15, Count: 2, Active: 1},
				Volumes:    docker.DiskGroup{Size: 1400, Count: 3, Active: 1, Reclaimable: 400},
				BuildCache: docker.DiskGroup{Size: 120, Count: 2},
				Total:      900 + 15 + 1400 + 120,
			},
		},
		{
			name: "the totals of a newer daemon",
			df: `{
				"ImageUsage": {"ActiveCount": 2, "TotalCount": 5, "TotalSize": 700, "Reclaimable": 300},
				"ContainerUsage": {"ActiveCount": 1, "TotalCount": 2, "TotalSize": 40},
				"VolumeUsage": {"ActiveCount": 0, "TotalCount": 1, "TotalSize": 60, "Reclaimable": 60},
				"BuildCacheUsage": {"TotalCount": 4, "TotalSize": 80}
			}`,
			want: docker.DiskUsage{
				Images:     docker.DiskGroup{Size: 700, Count: 5, Active: 2, Reclaimable: 300},
				Containers: docker.DiskGroup{Size: 40, Count: 2, Active: 1},
				Volumes:    docker.DiskGroup{Size: 60, Count: 1, Reclaimable: 60},
				BuildCache: docker.DiskGroup{Size: 80, Count: 4},
				Total:      700 + 40 + 60 + 80,
			},
		},
		{
			name: "a daemon with nothing",
			df:   `{"Images": [], "Containers": [], "Volumes": [], "BuildCache": [], "VolumeUsage": {}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := docker.NewHost(client(t, dfEngine(tt.df)))
			defer h.Close()
			got := h.Disk(t.Context(), 5*time.Second)
			if got == nil {
				t.Fatal("Disk() = nil")
			}
			got.At, got.Volume = time.Time{}, nil
			if !reflect.DeepEqual(*got, tt.want) {
				t.Errorf("Disk() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestHost_DiskVolumeSizes(t *testing.T) {
	t.Parallel()
	h := docker.NewHost(client(t, dfEngine(`{"Volumes": [
		{"Name": "data", "UsageData": {"Size": 1000, "RefCount": 1}},
		{"Name": "unmeasured", "UsageData": {"Size": -1, "RefCount": -1}},
		{"Name": "nodata"}
	]}`)))
	defer h.Close()
	got := h.Disk(t.Context(), 5*time.Second)
	if got == nil || len(got.Volume) != 1 || got.Volume["data"] != (docker.VolumeUse{Size: 1000, Refs: 1}) {
		t.Errorf("Disk().Volume = %+v, want only the measured volume", got)
	}
}

func TestHost_DiskTakesItsTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		e := newEngine()
		e.answer = func(w http.ResponseWriter, r *http.Request) bool {
			if !strings.HasSuffix(r.URL.Path, "/system/df") {
				return false
			}
			calls.Add(1)
			time.Sleep(20 * time.Second)
			_, _ = w.Write([]byte(`{"ImageUsage": {"TotalSize": 5}}`))
			return true
		}
		h := docker.NewHost(client(t, e))
		defer h.Close()

		// The page doesn't wait for a daemon that's slow: it gets nothing, and
		// the work goes on for the next time.
		if got := h.Disk(t.Context(), 2500*time.Millisecond); got != nil {
			t.Errorf("Disk() = %+v after 2.5 s, want nothing yet", got)
		}
		time.Sleep(30 * time.Second)
		got := h.Disk(t.Context(), 2500*time.Millisecond)
		if got == nil || got.Images.Size != 5 {
			t.Errorf("Disk() = %+v, want what the daemon finished", got)
		}
		if n := calls.Load(); n != 1 {
			t.Errorf("the daemon was asked %d times, want once", n)
		}
	})
}
