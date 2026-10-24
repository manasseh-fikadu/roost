-- How Roost logs in to each machine over SSH, for terminals, files and
-- Docker: by the device's ID, or 'hub' for the machine Roost runs on.
CREATE TABLE ssh_access (
    device_id  TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    port       INTEGER NOT NULL,
    host_key   TEXT,             -- the key pinned on the first connection, as in known_hosts, or NULL
    pinned_at  INTEGER,          -- Unix milliseconds
    created_at INTEGER NOT NULL, -- Unix milliseconds
    created_by TEXT NOT NULL     -- the admin's login, or '' if Roost set it up
) STRICT;
