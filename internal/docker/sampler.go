package docker

import (
	"context"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

const (
	// SampleInterval is how often a Sampler records what the containers use.
	SampleInterval = time.Minute
	// sampleDelay is how long after it starts a Sampler first looks, which
	// gives the checks of SSH access time to find which machines have Docker.
	sampleDelay = 20 * time.Second
)

// A Target is a machine whose containers a Sampler records.
type Target struct {
	// ID is the ID the machine's history is kept under: store.HubID for the
	// machine Roost runs on, and otherwise the device's.
	ID   string
	Host *Host
}

// SamplerOptions configure a Sampler.
type SamplerOptions struct {
	// Targets returns the machines to record now. Required.
	Targets func() []Target
	// Record keeps rows of history. Required.
	Record func(ctx context.Context, rows []store.ContainerMetricsRow) error
	// Logger receives what goes wrong. Nil discards it.
	Logger *slog.Logger
}

// A Sampler records what each running container uses, once a minute, so the
// container's page can chart where it has been.
type Sampler struct {
	opts SamplerOptions
	log  *slog.Logger

	// last holds each container's counters from the minute before, by target
	// and container ID, which the rates are worked out from. Each row is then
	// the average over the minute, not what happened in the second it was
	// taken.
	last map[string]Counters
	// failing holds the targets that failed the last time, so the log says so
	// once.
	failing map[string]bool
}

// NewSampler returns a Sampler. It panics if opts lacks a required field.
func NewSampler(opts SamplerOptions) *Sampler {
	if opts.Targets == nil || opts.Record == nil {
		panic("docker.NewSampler: SamplerOptions.Targets and Record are required")
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Sampler{opts: opts, log: log, last: map[string]Counters{}, failing: map[string]bool{}}
}

// Run records the containers every SampleInterval, until ctx ends.
func (s *Sampler) Run(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(sampleDelay):
	}
	t := time.NewTicker(SampleInterval)
	defer t.Stop()
	for {
		s.sample(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Sampler) sample(ctx context.Context) {
	targets := s.opts.Targets()
	var (
		mu   sync.Mutex
		rows []store.ContainerMetricsRow
		seen = map[string]Counters{}
		wg   sync.WaitGroup
	)
	for _, t := range targets {
		wg.Go(func() {
			got, counters, err := s.sampleTarget(ctx, t)
			mu.Lock()
			defer mu.Unlock()
			rows = append(rows, got...)
			maps.Copy(seen, counters)
			s.noteFailure(ctx, t.ID, err)
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return
	}
	// A container that's gone, or a machine that didn't answer, starts
	// afresh next time.
	s.last = seen
	if err := s.opts.Record(ctx, rows); err != nil && ctx.Err() == nil {
		s.log.Warn("record the containers' history", "error", err)
	}
}

// noteFailure logs that a target failed, or answered again, once.
func (s *Sampler) noteFailure(ctx context.Context, id string, err error) {
	switch {
	case err != nil && ctx.Err() == nil && !s.failing[id]:
		s.log.Warn("sample a machine's containers", "device", id, "error", err)
		s.failing[id] = true
	case err == nil && s.failing[id]:
		s.log.Info("sampled a machine's containers again", "device", id)
		delete(s.failing, id)
	}
}

// sampleTarget takes one sample of each running container of a machine.
func (s *Sampler) sampleTarget(ctx context.Context, t Target) ([]store.ContainerMetricsRow, map[string]Counters, error) {
	list, err := t.Host.Containers(ctx)
	if err != nil {
		return nil, nil, err
	}
	var running []RawContainer
	for _, c := range list {
		if c.State == "running" {
			running = append(running, c)
		}
	}
	var (
		mu       sync.Mutex
		rows     []store.ContainerMetricsRow
		counters = map[string]Counters{}
	)
	now := time.Now()
	forEach(len(running), func(i int) {
		c := running[i]
		raw, err := t.Host.Client().Sample(ctx, c.ID)
		if err != nil {
			// This minute has a gap for it.
			return
		}
		key := t.ID + "/" + c.ID
		var prev *Counters
		if p, ok := s.last[key]; ok {
			prev = &p
		}
		st, cur, ok := Derive(raw, prev)
		if !ok {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		counters[key] = cur
		rows = append(rows, store.ContainerMetricsRow{
			DeviceID: t.ID, Name: c.Name(), At: now,
			CPU: st.CPU, Mem: st.MemUsed, NetRx: st.NetRx, NetTx: st.NetTx,
			BlkRead: st.BlkReadRate, BlkWrite: st.BlkWriteRate,
		})
	})
	return rows, counters, nil
}
