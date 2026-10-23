package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// agentTokenKey is the kv key that holds the token agents send.
const agentTokenKey = "agent_token"

// AgentToken returns the token that agents send with each heartbeat, and
// makes one the first time. Every agent shares it until per-device
// enrollment (#8) replaces it.
func (s *Store) AgentToken(ctx context.Context) (string, error) {
	token := rand.Text()
	// A token that's already there stays, and the second statement reads
	// whichever token won.
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT (key) DO NOTHING", agentTokenKey, token); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "SELECT value FROM kv WHERE key = ?", agentTokenKey).Scan(&token)
	})
	if err != nil {
		return "", fmt.Errorf("get the agent token: %w", err)
	}
	return token, nil
}

// An AgentRecord is the latest report of a device's agent.
type AgentRecord struct {
	DeviceID   string
	ReceivedAt time.Time
	// Report is the report as package agents encodes it.
	Report []byte
}

// SaveAgent replaces the latest report of a device's agent.
func (s *Store) SaveAgent(ctx context.Context, rec AgentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (device_id, received_at, report) VALUES (?, ?, ?)
		ON CONFLICT (device_id) DO UPDATE SET received_at = excluded.received_at, report = excluded.report`,
		rec.DeviceID, rec.ReceivedAt.UnixMilli(), string(rec.Report))
	if err != nil {
		return fmt.Errorf("save an agent's report: %w", err)
	}
	return nil
}

// Agents returns the latest report of every device's agent.
func (s *Store) Agents(ctx context.Context) ([]AgentRecord, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT device_id, received_at, report FROM agents ORDER BY device_id")
	if err != nil {
		return nil, fmt.Errorf("read the agents' reports: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AgentRecord
	for rows.Next() {
		var (
			rec    AgentRecord
			at     int64
			report string
		)
		if err := rows.Scan(&rec.DeviceID, &at, &report); err != nil {
			return nil, fmt.Errorf("read the agents' reports: %w", err)
		}
		rec.ReceivedAt, rec.Report = time.UnixMilli(at), []byte(report)
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the agents' reports: %w", err)
	}
	return out, nil
}

// ErrNoAgent means a device has no agent that Roost knows of.
var ErrNoAgent = errors.New("no agent has reported from this device")

// ForgetAgent deletes the latest report of a device's agent. It returns
// ErrNoAgent if there's none.
func (s *Store) ForgetAgent(ctx context.Context, deviceID string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM agents WHERE device_id = ?", deviceID)
	if err != nil {
		return fmt.Errorf("forget an agent: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNoAgent
	}
	return nil
}
