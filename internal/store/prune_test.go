package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

func TestStore_retention(t *testing.T) {
	t.Parallel()
	const day = 24 * time.Hour
	defaults := store.DefaultRetention()
	for _, tt := range []struct {
		name string
		opts store.Options
		// want lists the pings that are left after an hour, by device ID, which
		// says how many days ago they were sent.
		want []string
	}{
		{"the defaults", store.Options{}, []string{"6d"}},
		{"a day", store.Options{Retention: fixedRetention(store.Retention{Activity: day, History: day, Checks: day})}, nil},
		{"a month", store.Options{Retention: fixedRetention(store.Retention{Activity: day, History: 30 * day, Checks: day})}, []string{"10d", "6d"}},
		{"a setting it can't read", store.Options{Retention: func(context.Context, *store.Store) (store.Retention, error) {
			return store.Retention{}, errors.New("no")
		}}, []string{"10d", "6d"}},
		{"nothing above zero", store.Options{Retention: fixedRetention(store.Retention{Activity: defaults.Activity})}, []string{"10d", "6d"}},
		{"no pruning", store.Options{NoPrune: true}, []string{"10d", "6d"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				s, err := store.OpenWith(t.Context(), filepath.Join(t.TempDir(), "roost.db"), tt.opts, store.Migrations())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = s.Close() })
				synctest.Wait()
				now := time.Now()
				// Sent a little under 10 and 6 days ago.
				err = s.AddPings(t.Context(), []store.Ping{
					{DeviceID: "10d", At: now.Add(-10*day + time.Hour)},
					{DeviceID: "6d", At: now.Add(-6*day + time.Hour)},
				})
				if err != nil {
					t.Fatal(err)
				}
				time.Sleep(store.PruneInterval)
				synctest.Wait()
				got := firstColumn(t, s.DB(), "SELECT device_id FROM pings ORDER BY device_id")
				if !slices.Equal(got, tt.want) {
					t.Errorf("pings left = %q, want %q", got, tt.want)
				}
			})
		})
	}
}

func fixedRetention(r store.Retention) func(context.Context, *store.Store) (store.Retention, error) {
	return func(context.Context, *store.Store) (store.Retention, error) { return r, nil }
}
