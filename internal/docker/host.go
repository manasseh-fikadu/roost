package docker

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	// listTTL and entriesTTL are how long the container list, and the list
	// with each container's details and stats, are good for. Pages ask every
	// few seconds, and these keep what they ask for from adding up.
	listTTL    = 2500 * time.Millisecond
	entriesTTL = 3 * time.Second
	infoTTL    = 30 * time.Second
	// diskTTL is how long disk usage is kept, since the daemon takes a while
	// to work it out.
	diskTTL = time.Minute
	// inspectTTL is how long a container's inspection is reused while its
	// state and status stay as they were.
	inspectTTL = 5 * time.Minute
	// counterTTL is how old a container's last counters can be and still make
	// rates for its next sample.
	counterTTL = 2 * time.Minute
	// coldDelay is how long a container Bield has no counters for waits for
	// its second sample.
	coldDelay = time.Second
	// parallel is how many requests a Host sends at once, which is gentle on
	// a socket that comes through SSH.
	parallel = 8
)

// A Host is the Docker of one machine: what's asked of it, cached for a few
// seconds so several viewers cost it one set of requests, and shared live
// stats. It is safe for concurrent use.
type Host struct {
	client *Client

	list    *cache[[]RawContainer]
	entries *cache[[]Entry]
	info    *cache[Info]
	disk    *cache[*DiskUsage]

	mu          sync.Mutex
	inspections map[string]inspection
	counters    map[string]Counters
	feeds       map[string]*feed
	closed      bool
}

type inspection struct {
	// key is the container's state and status when it was inspected.
	key  string
	at   time.Time
	data *RawInspect
}

// NewHost returns the Host that talks to Docker with client.
func NewHost(client *Client) *Host {
	return &Host{
		client:  client,
		list:    newCache[[]RawContainer](listTTL),
		entries: newCache[[]Entry](entriesTTL),
		info:    newCache[Info](infoTTL),
		disk:    newCache[*DiskUsage](diskTTL),
		feeds:   map[string]*feed{},
	}
}

// Client returns the Host's client, for requests it doesn't cache.
func (h *Host) Client() *Client { return h.client }

// Close ends the Host's live stats.
func (h *Host) Close() {
	h.mu.Lock()
	h.closed = true
	feeds := h.feeds
	h.feeds = map[string]*feed{}
	h.mu.Unlock()
	for _, f := range feeds {
		f.stop()
	}
}

// Containers returns every container, as the list says it.
func (h *Host) Containers(ctx context.Context) ([]RawContainer, error) {
	return h.list.get(ctx, h.client.List)
}

// An Entry is a container with what's known of it: its entry in the list, its
// inspection, and what it uses now.
type Entry struct {
	Container RawContainer
	// Inspect is nil if the container couldn't be inspected, such as when it
	// was removed since the list.
	Inspect *RawInspect
	// Stats is nil unless the container is running and Docker gave a sample.
	Stats *Stats
}

// Entries returns every container with its inspection and, if it's running,
// what it uses, by name.
func (h *Host) Entries(ctx context.Context) ([]Entry, error) {
	return h.entries.get(ctx, h.loadEntries)
}

func (h *Host) loadEntries(ctx context.Context) ([]Entry, error) {
	list, err := h.Containers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, len(list))
	var running []int
	for i, c := range list {
		out[i].Container = c
		if c.State == "running" {
			running = append(running, i)
		}
	}
	h.inspectAll(ctx, out)
	h.sampleAll(ctx, out, running)
	return out, nil
}

// inspectAll fills in the inspection of each entry. A container's state and
// status change whenever anything about it that's worth inspecting again
// does, such as "Up 6 hours" turning into "Up 2 seconds" after a restart, so
// they're a cheap key for reusing an inspection.
func (h *Host) inspectAll(ctx context.Context, entries []Entry) {
	h.mu.Lock()
	prev := h.inspections
	h.mu.Unlock()
	next := make(map[string]inspection, len(entries))
	var mu sync.Mutex
	forEach(len(entries), func(i int) {
		c := entries[i].Container
		key := c.State + "|" + c.Status
		hit, ok := prev[c.ID]
		if ok && hit.key == key && time.Since(hit.at) < inspectTTL {
			entries[i].Inspect = hit.data
			mu.Lock()
			next[c.ID] = hit
			mu.Unlock()
			return
		}
		data, err := h.client.Inspect(ctx, c.ID)
		if err != nil {
			if ok {
				entries[i].Inspect = hit.data
				mu.Lock()
				next[c.ID] = hit
				mu.Unlock()
			}
			return
		}
		entries[i].Inspect = data
		mu.Lock()
		next[c.ID] = inspection{key: key, at: time.Now(), data: data}
		mu.Unlock()
	})
	h.mu.Lock()
	h.inspections = next
	h.mu.Unlock()
}

// sampleAll fills in what each running entry uses. Rates need two samples, so
// it keeps each container's last counters, and a refresh takes one quick
// sample of each. A container it hasn't seen before gets a second sample a
// moment later.
func (h *Host) sampleAll(ctx context.Context, entries []Entry, running []int) {
	h.mu.Lock()
	prev := h.counters
	h.mu.Unlock()
	base := make([]*Counters, len(running))
	first := make([]*RawStats, len(running))
	forEach(len(running), func(n int) {
		id := entries[running[n]].Container.ID
		if raw, err := h.client.Sample(ctx, id); err == nil {
			first[n] = raw
		}
		if c, ok := prev[id]; ok && time.Since(c.seen) < counterTTL {
			base[n] = &c
		}
	})
	var cold []int
	for n := range running {
		if first[n] != nil && base[n] == nil {
			cold = append(cold, n)
		}
	}
	if len(cold) > 0 {
		select {
		case <-time.After(coldDelay):
		case <-ctx.Done():
			return
		}
		second := make([]*RawStats, len(cold))
		forEach(len(cold), func(k int) {
			id := entries[running[cold[k]]].Container.ID
			if raw, err := h.client.Sample(ctx, id); err == nil {
				second[k] = raw
			}
		})
		for k, n := range cold {
			if second[k] == nil {
				continue
			}
			if _, c, ok := Derive(first[n], nil); ok {
				base[n] = &c
			}
			first[n] = second[k]
		}
	}
	next := make(map[string]Counters, len(running))
	for n, i := range running {
		if first[n] == nil {
			continue
		}
		if s, c, ok := Derive(first[n], base[n]); ok {
			entries[i].Stats = &s
			next[entries[i].Container.ID] = c
		}
	}
	h.mu.Lock()
	h.counters = next
	h.mu.Unlock()
}

// forEach calls fn for each i below n, a few at a time, and waits for them.
func forEach(n int, fn func(i int)) {
	var wg sync.WaitGroup
	slots := make(chan struct{}, parallel)
	for i := range n {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			fn(i)
		})
	}
	wg.Wait()
}

// Info is what the daemon says of itself.
type Info struct {
	RawInfo
	// APIVersion is the Engine API version it speaks, or empty.
	APIVersion string
}

// Info returns what the daemon says of itself.
func (h *Host) Info(ctx context.Context) (Info, error) {
	return h.info.get(ctx, func(ctx context.Context) (Info, error) {
		raw, err := h.client.Info(ctx)
		if err != nil {
			return Info{}, err
		}
		// A daemon that won't say its API version still has an overview.
		v, _ := h.client.APIVersion(ctx)
		return Info{RawInfo: *raw, APIVersion: v}, nil
	})
}

// Disk returns the disk space the daemon uses, if it works it out within
// wait. The daemon goes on in the background if it doesn't, and a later call
// has it.
func (h *Host) Disk(ctx context.Context, wait time.Duration) *DiskUsage {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	d, err := h.disk.get(ctx, func(ctx context.Context) (*DiskUsage, error) {
		raw, err := h.client.Df(ctx)
		if err != nil {
			return nil, err
		}
		return parseDf(raw, time.Now()), nil
	})
	if err != nil {
		return nil
	}
	return d
}

// Do starts, stops or restarts a container, and reports whether anything
// changed. Once it asks, it goes on if ctx ends: Docker acts on the
// container anyway, and Bield should hear what happened.
func (h *Host) Do(ctx context.Context, id string, a Action) (bool, error) {
	changed, err := h.client.Do(context.WithoutCancel(ctx), id, a)
	// The next look at the containers shows what happened.
	h.list.forget()
	h.entries.forget()
	return changed, err
}

// Name returns a container's name, given its ID, a prefix of it, or its name.
func (h *Host) Name(ctx context.Context, id string) (string, error) {
	if list, err := h.Containers(ctx); err == nil {
		for _, c := range list {
			if c.ID == id || (len(id) >= 4 && strings.HasPrefix(c.ID, id)) || name(c) == id {
				return name(c), nil
			}
		}
	}
	in, err := h.client.Inspect(ctx, id)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(in.Name, "/"), nil
}

// name returns a container's name, without the slash that Docker puts in
// front of it.
func name(c RawContainer) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return c.ID[:min(len(c.ID), 12)]
}

// Name returns the container's name.
func (c RawContainer) Name() string { return name(c) }
