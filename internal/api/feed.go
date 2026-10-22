package api

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/manasseh-fikadu/roost/internal/hostmetrics"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

const (
	// feedGap is the least time between two snapshots. Changes that come
	// closer together share one.
	feedGap = time.Second
	// feedHeartbeat is the longest the feed goes without a new snapshot, so
	// clients can tell a quiet stream from a dead one.
	feedHeartbeat = 15 * time.Second
)

// FeedOptions configures NewFeed.
type FeedOptions struct {
	// Version is the hub's build version.
	Version string
	// Tailnet returns what Roost last learned about the tailnet. Required.
	Tailnet func() tailnet.Snapshot
	// TailnetChanged receives a value when Tailnet's answer changes.
	TailnetChanged <-chan struct{}
	// Host returns what Roost last measured of its own machine. It's nil if
	// Roost can't measure it.
	Host func() hostmetrics.Reading
	// HostChanged receives a value when Host's answer changes.
	HostChanged <-chan struct{}
	// Store holds the devices' settings and history. Required.
	Store *store.Store
	// Logger receives errors. Nil discards them.
	Logger *slog.Logger
}

// Feed builds the snapshots that GET /api/stream sends: one each time what
// Roost knows changes, rather than one for each client. Each is encoded and
// compressed once, for admins and once for everyone else, and every client
// gets the same bytes.
//
// It also records each ping of a peer, for the device's history.
type Feed struct {
	opts  FeedOptions
	log   *slog.Logger
	poked chan struct{}

	// buildMu is held while a snapshot is built, and guards the fields up
	// to mu.
	buildMu     sync.Mutex
	zw          *flate.Writer
	settings    map[string]store.DeviceSettings
	settingsErr error
	// pingsUpTo is when the latest recorded ping was sent.
	pingsUpTo time.Time
	pingsErr  error

	mu  sync.Mutex
	cur *published
}

// published is a snapshot, ready to send.
type published struct {
	// snap is the snapshot as admins see it.
	snap Snapshot
	// admin and viewer are the "snapshot" event for admins and for everyone
	// else.
	admin, viewer *event
	// next is closed when a newer snapshot is published.
	next chan struct{}
}

// NewFeed returns a Feed. It builds a snapshot the first time one is asked
// for, and then each time something changes while Run runs. It panics if
// opts lacks a required field.
func NewFeed(opts FeedOptions) *Feed {
	if opts.Tailnet == nil || opts.Store == nil {
		panic("api.NewFeed: FeedOptions.Tailnet and Store are required")
	}
	f := &Feed{opts: opts, log: opts.Logger, poked: make(chan struct{}, 1)}
	if f.log == nil {
		f.log = slog.New(slog.DiscardHandler)
	}
	return f
}

// Run builds a snapshot now, and then each time what Roost knows changes,
// until ctx ends.
func (f *Feed) Run(ctx context.Context) {
	heartbeat := time.NewTimer(feedHeartbeat)
	defer heartbeat.Stop()
	gap := time.NewTimer(0)
	defer gap.Stop()
	for {
		builtAt := time.Now()
		f.publish(ctx)
		heartbeat.Reset(feedHeartbeat)
		select {
		case <-ctx.Done():
			return
		case <-f.opts.TailnetChanged:
		case <-f.opts.HostChanged:
		case <-f.poked:
		case <-heartbeat.C:
		}
		gap.Reset(time.Until(builtAt.Add(feedGap)))
		select {
		case <-ctx.Done():
			return
		case <-gap.C:
		}
		// The next snapshot has every change made while waiting.
		f.drain()
	}
}

// drain takes the changes waiting to be heard of.
func (f *Feed) drain() {
	for _, ch := range []<-chan struct{}{f.opts.TailnetChanged, f.opts.HostChanged, f.poked} {
		select {
		case <-ch:
		default:
		}
	}
}

// Poke makes Run build a new snapshot soon, such as after an admin changes a
// device's settings.
func (f *Feed) Poke() {
	select {
	case f.poked <- struct{}{}:
	default:
	}
}

// latest returns the latest snapshot, building the first one if there's
// none yet. It returns nil if the first can't be encoded.
func (f *Feed) latest(ctx context.Context) *published {
	f.mu.Lock()
	p := f.cur
	f.mu.Unlock()
	if p != nil {
		return p
	}
	return f.publish(ctx)
}

// publish builds a snapshot and makes it the latest, and returns it. If it
// can't encode the snapshot, it keeps the previous one and returns that.
func (f *Feed) publish(ctx context.Context) *published {
	f.buildMu.Lock()
	defer f.buildMu.Unlock()
	src := sources{version: f.opts.Version, tailnet: f.opts.Tailnet()}
	if f.opts.Host != nil {
		h := f.opts.Host()
		src.host = &h
	}
	f.recordPings(ctx, src.tailnet.Pings)
	src.settings = f.loadSettings(ctx)

	snap := newSnapshot(src, time.Now())
	p := &published{snap: snap, next: make(chan struct{})}
	admin, err := f.newEvent("snapshot", snap)
	if err == nil {
		p.admin = admin
		p.viewer, err = f.newEvent("snapshot", redact(snap))
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		f.log.Error("encode a snapshot", "error", err)
		return f.cur
	}
	if f.cur != nil {
		close(f.cur.next)
	}
	f.cur = p
	return p
}

// An event is a server-sent event, ready to write.
type event struct {
	// raw is the event as text.
	raw []byte
	// deflated is raw compressed with DEFLATE on its own, ending on a byte
	// boundary with a sync flush. It refers to nothing before it, so it can
	// follow any other event's deflated bytes in a gzip stream.
	deflated []byte
}

// newEvent encodes v as the data of an event called name.
func (f *Feed) newEvent(name string, v any) (*event, error) {
	// JSON escapes every newline inside strings, so data fits on one line.
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	raw := fmt.Appendf(nil, "event: %s\ndata: %s\n\n", name, data)
	var buf bytes.Buffer
	// Reset forgets everything written before, so the event stands alone.
	if f.zw == nil {
		if f.zw, err = flate.NewWriter(&buf, flate.DefaultCompression); err != nil {
			return nil, err
		}
	} else {
		f.zw.Reset(&buf)
	}
	if _, err := f.zw.Write(raw); err != nil {
		return nil, err
	}
	// Flush, unlike Close, doesn't end the stream with a final block.
	if err := f.zw.Flush(); err != nil {
		return nil, err
	}
	return &event{raw: raw, deflated: buf.Bytes()}, nil
}

// recordPings stores the pings sent since the last ones it stored. A ping
// that failed on Roost's side says nothing about the device, so it's left
// out.
func (f *Feed) recordPings(ctx context.Context, pings map[string]tailnet.PingRecord) {
	var (
		batch []store.Ping
		upTo  = f.pingsUpTo
	)
	for id, rec := range pings {
		if !rec.At.After(f.pingsUpTo) {
			continue
		}
		if rec.At.After(upTo) {
			upTo = rec.At
		}
		p := store.Ping{DeviceID: id, At: rec.At}
		switch {
		case rec.Result != nil:
			p.Replied, p.RTT = true, rec.Result.Latency
		case !errors.Is(rec.Err, tailnet.ErrNoReply):
			continue
		}
		batch = append(batch, p)
	}
	if len(batch) == 0 {
		f.pingsUpTo = upTo
		return
	}
	if err := f.opts.Store.AddPings(ctx, batch); err != nil {
		if ctx.Err() == nil && f.pingsErr == nil {
			f.log.Warn("record pings", "error", err)
		}
		f.pingsErr = err
		return
	}
	if f.pingsErr != nil {
		f.log.Info("recorded pings again")
	}
	f.pingsUpTo, f.pingsErr = upTo, nil
}

// loadSettings returns the devices' settings, or the last ones it read if
// the store can't say.
func (f *Feed) loadSettings(ctx context.Context) map[string]store.DeviceSettings {
	settings, err := f.opts.Store.DeviceSettings(ctx)
	if err != nil {
		if ctx.Err() == nil && f.settingsErr == nil {
			f.log.Warn("read device settings", "error", err)
		}
		f.settingsErr = err
		return f.settings
	}
	if f.settingsErr != nil {
		f.log.Info("read device settings again")
	}
	f.settings, f.settingsErr = settings, nil
	return settings
}
