-- In the tables below, device_id is a device's Tailscale node ID, or 'hub'
-- for the machine Roost runs on, whichever node it appears as.

-- A device's metrics, about every 30 seconds, from Roost's own measurements
-- or from the device's agent. A value is NULL if the device didn't report it.
CREATE TABLE metrics (
    ts        INTEGER NOT NULL, -- Unix milliseconds
    device_id TEXT NOT NULL,
    cpu       REAL, -- percent busy
    mem       REAL, -- percent of memory used
    disk      REAL, -- percent of / used
    load1     REAL, -- 1-minute load average
    net_rx    REAL, -- bytes per second
    net_tx    REAL,
    temp      REAL, -- degrees Celsius
    battery   REAL  -- percent charged
) STRICT;

CREATE INDEX metrics_device_ts ON metrics (device_id, ts);

-- The latest report of each device's agent, so Roost still knows a device's
-- metrics and details after it restarts.
CREATE TABLE agents (
    device_id   TEXT PRIMARY KEY,
    received_at INTEGER NOT NULL, -- Unix milliseconds
    report      TEXT NOT NULL     -- JSON, as package agents writes it
) STRICT;

-- What used each device's CPU and memory, summed up once a minute.
CREATE TABLE usage_samples (
    ts        INTEGER NOT NULL, -- Unix milliseconds, at the end of the minute
    device_id TEXT NOT NULL,
    source    TEXT NOT NULL, -- 'hub' or 'agent'
    cores     INTEGER,
    -- JSON: the biggest groups of processes and what each used. History reads
    -- it for every minute in a range, so it's kept small.
    groups    TEXT NOT NULL,
    -- JSON compressed with DEFLATE: groups and processes in detail, for
    -- showing one minute. After a day, only one minute in ten keeps it.
    detail    BLOB
) STRICT;

CREATE INDEX usage_samples_device_ts ON usage_samples (device_id, ts);
