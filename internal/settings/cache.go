package settings

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

// cacheFor is how long a Cache trusts what it read from the store.
const cacheFor = 2 * time.Second

// A Cache gives the settings to code that asks for them often, such as each
// sample of the machine's CPU, without reading the store every time. A change
// made through the Cache shows up at once, and one made another way, such as
// by editing the database, within a few seconds. It's safe for concurrent
// use.
type Cache struct {
	st  *store.Store
	log *slog.Logger

	mu  sync.Mutex
	cur Settings
	// at is when cur was read, or zero before the first read.
	at time.Time
	// failing reports whether the latest read failed.
	failing bool
}

// NewCache returns a Cache of the settings in st. Nothing is read until the
// first Get.
func NewCache(st *store.Store, log *slog.Logger) *Cache {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Cache{st: st, log: log, cur: Defaults()}
}

// Get returns the settings. If the store can't say, it returns the last ones
// it read, or the defaults if it never has.
func (c *Cache) Get(ctx context.Context) Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at.IsZero() || time.Since(c.at) >= cacheFor {
		c.read(ctx)
	}
	return c.cur.clone()
}

// read reads the settings from the store, if it can. The caller holds mu.
func (c *Cache) read(ctx context.Context) {
	s, err := Load(ctx, c.st)
	switch {
	case err != nil && ctx.Err() == nil && !c.failing:
		c.log.Warn("read the settings", "error", err)
	case err == nil && c.failing:
		c.log.Info("read the settings again")
	}
	c.failing = err != nil
	if err == nil {
		c.cur = s
	}
	// A failed read counts too, so a broken store isn't asked on every call.
	if ctx.Err() == nil {
		c.at = time.Now()
	}
}

// Update changes the settings: it reads the stored ones, calls change on
// them, saves what it left if that passes Check, and returns the settings
// from before and after. Updates don't overlap, so none loses another's
// change. If change, Check or the store fails, nothing is saved. Saved
// settings that can't be read, as if they were edited by hand, count as the
// defaults, so that an admin can write them over.
func (c *Cache) Update(ctx context.Context, change func(*Settings) error) (before, after Settings, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	before, err = Load(ctx, c.st)
	if errors.Is(err, ErrCorrupt) {
		c.log.Warn("writing over settings that can't be read", "error", err)
		err = nil
	}
	if err != nil {
		return Settings{}, Settings{}, err
	}
	after = before.clone()
	if err := change(&after); err != nil {
		return Settings{}, Settings{}, err
	}
	if err := Save(ctx, c.st, after); err != nil {
		return Settings{}, Settings{}, err
	}
	c.cur, c.at, c.failing = after.clone(), time.Now(), false
	return before, after, nil
}

// clone returns a copy of s that shares nothing with it.
func (s Settings) clone() Settings {
	s.WatchedUnits = append([]string{}, s.WatchedUnits...)
	return s
}
