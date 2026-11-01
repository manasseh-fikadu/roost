-- What admins set for each device, by its Tailscale node ID. A device with
-- no row has the defaults.
CREATE TABLE device_settings (
    device_id     TEXT PRIMARY KEY,
    display_name  TEXT NOT NULL DEFAULT '', -- '' shows the name the device reports
    kind          TEXT NOT NULL DEFAULT '', -- '' lets Bield guess from the device
    alert_offline INTEGER,                  -- 0 or 1, or NULL for the default
    notes         TEXT NOT NULL DEFAULT ''
) STRICT;

-- Every ping of a peer, for its round-trip time history.
CREATE TABLE pings (
    ts        INTEGER NOT NULL, -- Unix milliseconds
    device_id TEXT NOT NULL,
    rtt_ms    REAL              -- NULL if no reply came back
) STRICT;

CREATE INDEX pings_device_ts ON pings (device_id, ts);
