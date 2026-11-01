package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrNoChannel means no notification channel has the ID.
	ErrNoChannel = errors.New("no notification channel has this ID")
	// ErrChannelNameTaken means another notification channel has the name.
	ErrChannelNameTaken = errors.New("another notification channel has this name")
)

// A Channel is a place where Bield sends notifications, such as an ntfy topic.
type Channel struct {
	ID   int64
	Name string
	// Kind says what sort of place it is, such as "ntfy".
	Kind    string
	Enabled bool
	// MinSeverity is the least severe alert it hears about: "info",
	// "warning" or "critical".
	MinSeverity string
	// OnResolve is whether it also hears when an alert resolves.
	OnResolve bool
	// Config is JSON that says where and how to send, including secrets.
	// Its shape belongs to the notify package.
	Config  string
	Created time.Time
}

// A Notified says what a channel was last told about an alert.
type Notified struct {
	AlertID, ChannelID int64
	// Severity is the severity of the alert at the last message.
	Severity string
	// At is when the last message was sent.
	At time.Time
}

// Channels returns every notification channel, by name.
func (s *Store) Channels(ctx context.Context) ([]Channel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, kind, enabled, min_severity, on_resolve, config, created_at
		FROM notification_channels ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("read the notification channels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Channel
	for rows.Next() {
		var (
			c       Channel
			created int64
		)
		err := rows.Scan(&c.ID, &c.Name, &c.Kind, &c.Enabled, &c.MinSeverity, &c.OnResolve, &c.Config, &created)
		if err != nil {
			return nil, fmt.Errorf("read the notification channels: %w", err)
		}
		c.Created = time.UnixMilli(created)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the notification channels: %w", err)
	}
	return out, nil
}

// AddChannel saves a new channel, and returns it with its ID. It returns
// ErrChannelNameTaken if another channel has the name, in any case.
func (s *Store) AddChannel(ctx context.Context, c Channel) (Channel, error) {
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO notification_channels (name, kind, enabled, min_severity, on_resolve, config, created_at)
		SELECT ?1, ?2, ?3, ?4, ?5, ?6, ?7
		WHERE NOT EXISTS (SELECT 1 FROM notification_channels WHERE name = ?1 COLLATE NOCASE)
		RETURNING id`,
		c.Name, c.Kind, c.Enabled, c.MinSeverity, c.OnResolve, c.Config, c.Created.UnixMilli()).Scan(&c.ID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Channel{}, ErrChannelNameTaken
	case err != nil:
		return Channel{}, fmt.Errorf("add a notification channel: %w", err)
	}
	return c, nil
}

// UpdateChannel replaces the channel with c's ID, except when it was
// created. It returns ErrNoChannel if there's none, and ErrChannelNameTaken
// if another channel has the name.
func (s *Store) UpdateChannel(ctx context.Context, c Channel) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE notification_channels SET name = ?2, kind = ?3, enabled = ?4, min_severity = ?5,
			on_resolve = ?6, config = ?7
		WHERE id = ?1
			AND NOT EXISTS (SELECT 1 FROM notification_channels WHERE name = ?2 COLLATE NOCASE AND id != ?1)`,
		c.ID, c.Name, c.Kind, c.Enabled, c.MinSeverity, c.OnResolve, c.Config)
	if err != nil {
		return fmt.Errorf("change a notification channel: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("change a notification channel: %w", err)
	}
	if n > 0 {
		return nil
	}
	// Either there's no such channel, or another has the name.
	var one int
	err = s.db.QueryRowContext(ctx, "SELECT 1 FROM notification_channels WHERE id = ?", c.ID).Scan(&one)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrNoChannel
	case err != nil:
		return fmt.Errorf("change a notification channel: %w", err)
	}
	return ErrChannelNameTaken
}

// DeleteChannel deletes a channel and what it was told. It returns
// ErrNoChannel if there's none.
func (s *Store) DeleteChannel(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM notification_channels WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete a notification channel: %w", err)
	}
	n, err := res.RowsAffected()
	switch {
	case err != nil:
		return fmt.Errorf("delete a notification channel: %w", err)
	case n == 0:
		return ErrNoChannel
	}
	return nil
}

// Notified returns what every channel was last told about each alert that
// it hasn't been told resolved.
func (s *Store) Notified(ctx context.Context) ([]Notified, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT alert_id, channel_id, severity, notified_at FROM notification_state")
	if err != nil {
		return nil, fmt.Errorf("read what channels were told: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Notified
	for rows.Next() {
		var (
			n  Notified
			at int64
		)
		if err := rows.Scan(&n.AlertID, &n.ChannelID, &n.Severity, &at); err != nil {
			return nil, fmt.Errorf("read what channels were told: %w", err)
		}
		n.At = time.UnixMilli(at)
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read what channels were told: %w", err)
	}
	return out, nil
}

// SetNotified records that a channel was told about an alert. It does
// nothing if the alert or the channel has gone since.
func (s *Store) SetNotified(ctx context.Context, n Notified) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_state (alert_id, channel_id, severity, notified_at)
		SELECT ?1, ?2, ?3, ?4
		WHERE EXISTS (SELECT 1 FROM alerts WHERE id = ?1)
			AND EXISTS (SELECT 1 FROM notification_channels WHERE id = ?2)
		ON CONFLICT (alert_id, channel_id) DO UPDATE SET severity = excluded.severity,
			notified_at = excluded.notified_at`,
		n.AlertID, n.ChannelID, n.Severity, n.At.UnixMilli())
	if err != nil {
		return fmt.Errorf("record what a channel was told: %w", err)
	}
	return nil
}

// ClearNotified forgets what a channel was told about an alert.
func (s *Store) ClearNotified(ctx context.Context, alertID, channelID int64) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM notification_state WHERE alert_id = ? AND channel_id = ?", alertID, channelID)
	if err != nil {
		return fmt.Errorf("forget what a channel was told: %w", err)
	}
	return nil
}
