-- Alerts: each is a condition, such as a disk that's nearly full, from when
-- Bield saw it until it cleared. A condition that comes back opens a new one.
CREATE TABLE alerts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    key           TEXT NOT NULL, -- the condition, such as 'disk:hub'
    source        TEXT NOT NULL, -- what it's about, such as 'metrics' or 'check'
    device_id     TEXT,          -- the device it's on, if any
    check_id      TEXT,          -- the check it's about, if any
    severity      TEXT NOT NULL, -- 'critical', 'warning' or 'info'
    title         TEXT NOT NULL,
    detail        TEXT NOT NULL,
    opened_at     INTEGER NOT NULL, -- Unix milliseconds
    last_seen     INTEGER NOT NULL, -- when Bield last saw the condition
    resolved_at   INTEGER,          -- NULL while it's open
    acked_at      INTEGER,
    acked_by      TEXT,
    snoozed_until INTEGER,
    snoozed_by    TEXT
) STRICT;

-- A condition has at most one open alert.
CREATE UNIQUE INDEX alerts_open_key ON alerts (key) WHERE resolved_at IS NULL;
CREATE INDEX alerts_resolved_at ON alerts (resolved_at);

-- What admins wrote on an alert. Notes go when their alert does.
CREATE TABLE alert_notes (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_id INTEGER NOT NULL REFERENCES alerts (id) ON DELETE CASCADE,
    ts       INTEGER NOT NULL, -- Unix milliseconds
    author   TEXT NOT NULL,    -- the admin's login
    text     TEXT NOT NULL
) STRICT;

CREATE INDEX alert_notes_alert ON alert_notes (alert_id, id);

-- Who did what the activity log says, if someone did: a login, or '' for
-- something Bield saw happen.
ALTER TABLE events ADD COLUMN actor TEXT NOT NULL DEFAULT '';
