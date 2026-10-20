-- Small settings and state that don't need a table of their own.
CREATE TABLE kv (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;

-- The activity log. AUTOINCREMENT keeps IDs rising after old rows are pruned,
-- so a client can page through the log by ID.
CREATE TABLE events (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    ts        INTEGER NOT NULL, -- Unix milliseconds
    level     TEXT NOT NULL,
    kind      TEXT NOT NULL,
    message   TEXT NOT NULL,
    device_id TEXT
) STRICT;

CREATE INDEX events_ts ON events (ts);
