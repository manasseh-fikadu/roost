-- What each Docker container used, once a minute, so its page can chart the
-- past. It's kept by the container's name, not its ID, since Docker Compose
-- makes a new container with a new ID each time it deploys. device_id is as in
-- metrics. A value is NULL if Docker didn't say it.
CREATE TABLE container_metrics (
    ts        INTEGER NOT NULL, -- Unix milliseconds
    device_id TEXT NOT NULL,
    name      TEXT NOT NULL,
    cpu       REAL, -- percent of one CPU
    mem       REAL, -- bytes in use
    net_rx    REAL, -- bytes per second
    net_tx    REAL,
    blk_read  REAL, -- bytes per second
    blk_write REAL
) STRICT;

CREATE INDEX container_metrics_key ON container_metrics (device_id, name, ts);
