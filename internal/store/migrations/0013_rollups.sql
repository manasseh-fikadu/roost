-- Metrics and round-trip times of devices, summed up so Roost can keep them
-- for months. Raw samples last 48 hours. Roost folds them into 5-minute
-- buckets, and those into hourly buckets, which package store's rollup.go does
-- every few minutes. A bucket is complete when it's written, and it is
-- written once its time is over.
--
-- In the tables below, device_id is as in metrics and pings, step is the
-- bucket's length in seconds, and ts is when it starts, in Unix milliseconds.

CREATE TABLE metrics_rollup (
    device_id TEXT    NOT NULL,
    step      INTEGER NOT NULL,
    ts        INTEGER NOT NULL,
    n         INTEGER NOT NULL, -- the raw samples in it, which weigh the averages of a longer bucket
    -- Averages over the raw samples that had the value, as in metrics.
    cpu       REAL,
    mem       REAL,
    disk      REAL,
    load1     REAL,
    net_rx    REAL,
    net_tx    REAL,
    temp      REAL,
    battery   REAL,
    PRIMARY KEY (device_id, step, ts)
) STRICT, WITHOUT ROWID;

-- Finds the buckets of a span of time, for folding them and for deleting old
-- ones, without reading every device's.
CREATE INDEX metrics_rollup_step_ts ON metrics_rollup (step, ts);

CREATE TABLE pings_rollup (
    device_id TEXT    NOT NULL,
    step      INTEGER NOT NULL,
    ts        INTEGER NOT NULL,
    pings     INTEGER NOT NULL, -- how many were sent
    replies   INTEGER NOT NULL, -- how many got a reply
    rtt_ms    REAL,             -- average of the replies, or NULL if none came
    PRIMARY KEY (device_id, step, ts)
) STRICT, WITHOUT ROWID;

CREATE INDEX pings_rollup_step_ts ON pings_rollup (step, ts);

-- Folding the raw samples of everyone, and deleting the old ones, look at a
-- span of time and not at a device.
CREATE INDEX metrics_ts ON metrics (ts);
CREATE INDEX pings_ts ON pings (ts);
