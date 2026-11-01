package usage

import (
	"bytes"
	"compress/flate"
	"encoding/json"
	"io"
	"testing"
)

// sampleDetail returns what a busy machine's minute has: a group and a few
// processes for each of 40 services.
func sampleDetail() detail {
	var d detail
	for i := range 40 {
		name := "service-" + string(rune('a'+i%26)) + ".service"
		d.Groups = append(d.Groups, groupDetail{
			Procs: 3, Top: []string{"worker", "main"},
		})
		d.Groups[i].Key, d.Groups[i].CPU, d.Groups[i].Mem = "service:"+name, float64(i)/10, uint64(i)<<20
		d.Procs = append(d.Procs, procDetail{PID: 1000 + i, Name: "worker", User: "root", Cmd: "/usr/bin/worker --serve", CPU: float64(i) / 20, Mem: uint64(i) << 18, Group: "service:" + name})
	}
	return d
}

func TestDeflate_roundTrip(t *testing.T) {
	t.Parallel()
	d := sampleDetail()
	// Twice, so the second uses a writer that's been used.
	for range 2 {
		packed, err := deflate(d)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(flate.NewReader(bytes.NewReader(packed)))
		if err != nil {
			t.Fatal(err)
		}
		var got detail
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Groups) != len(d.Groups) || len(got.Procs) != len(d.Procs) || got.Procs[39].PID != 1039 {
			t.Fatalf("round trip = %d groups and %d processes, want %d and %d", len(got.Groups), len(got.Procs), len(d.Groups), len(d.Procs))
		}
	}
}

// BenchmarkDeflate packs a minute for storing, as Bield does for each device
// each minute.
func BenchmarkDeflate(b *testing.B) {
	d := sampleDetail()
	for b.Loop() {
		if _, err := deflate(d); err != nil {
			b.Fatal(err)
		}
	}
}
