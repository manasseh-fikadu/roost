-- The checks admins set up, which the hub runs every so often: an HTTP
-- request, a TCP connection or a DNS lookup. Roost also shows its pings of
-- each peer as a check, but those aren't stored here.
CREATE TABLE checks (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    kind          TEXT NOT NULL, -- 'http', 'tcp' or 'dns'
    target        TEXT NOT NULL, -- a URL, host:port or host name
    grp           TEXT NOT NULL, -- the group it's listed in
    interval_ms   INTEGER NOT NULL,
    timeout_ms    INTEGER NOT NULL,
    expect_status INTEGER,                   -- NULL accepts any HTTP status below 400
    insecure      INTEGER NOT NULL DEFAULT 0, -- 1 skips checking the TLS certificate
    paused        INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL           -- Unix milliseconds
) STRICT;

-- Each time a check ran. A check's results go when it does.
CREATE TABLE check_results (
    ts         INTEGER NOT NULL, -- Unix milliseconds, when it started
    check_id   TEXT NOT NULL REFERENCES checks (id) ON DELETE CASCADE,
    ok         INTEGER NOT NULL, -- 0 or 1
    latency_ms REAL,             -- NULL if nothing answered
    detail     TEXT NOT NULL     -- what happened, such as 'HTTP 200'
) STRICT;

CREATE INDEX check_results_check_ts ON check_results (check_id, ts);

-- The health score, once a minute while there is one.
CREATE TABLE health (
    ts          INTEGER PRIMARY KEY, -- Unix milliseconds
    score       REAL NOT NULL,       -- percent of the live checks that passed
    passing     INTEGER NOT NULL,
    failing     INTEGER NOT NULL,
    unavailable INTEGER NOT NULL
) STRICT;

-- A new hub starts with checks of its own internet connection, which say
-- nothing about whoever runs it. Admins can change or delete them.
INSERT INTO checks (id, name, kind, target, grp, interval_ms, timeout_ms, expect_status, created_at) VALUES
    (lower(hex(randomblob(8))), 'Cloudflare edge', 'tcp', '1.1.1.1:443', 'Internet', 30000, 3000, NULL, unixepoch() * 1000),
    (lower(hex(randomblob(8))), 'DNS resolution', 'dns', 'cloudflare.com', 'Internet', 60000, 3000, NULL, unixepoch() * 1000),
    (lower(hex(randomblob(8))), 'HTTPS egress', 'http', 'https://www.google.com/generate_204', 'Internet', 60000, 5000, 204, unixepoch() * 1000);
