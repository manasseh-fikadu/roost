-- Where Roost sends a notification when an alert opens, and what each one
-- has been told. The config is JSON, and holds the channel's secrets, such as
-- a webhook URL or a bot token.
CREATE TABLE notification_channels (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,    -- 'ntfy', 'discord', 'slack', 'telegram', 'email' or 'webhook'
    enabled      INTEGER NOT NULL, -- 0 or 1
    min_severity TEXT NOT NULL,    -- the least severe alert it hears about
    on_resolve   INTEGER NOT NULL, -- whether it hears when an alert resolves
    config       TEXT NOT NULL,
    created_at   INTEGER NOT NULL  -- Unix milliseconds
) STRICT;

CREATE UNIQUE INDEX notification_channels_name ON notification_channels (name COLLATE NOCASE);

-- What a channel was last told about an alert, so a restart doesn't repeat it
-- and Roost knows when to remind. A row goes when its alert resolves and the
-- channel has been told, or when either is deleted.
CREATE TABLE notification_state (
    alert_id    INTEGER NOT NULL REFERENCES alerts (id) ON DELETE CASCADE,
    channel_id  INTEGER NOT NULL REFERENCES notification_channels (id) ON DELETE CASCADE,
    severity    TEXT NOT NULL,    -- the severity the channel was last told
    notified_at INTEGER NOT NULL, -- Unix milliseconds
    PRIMARY KEY (alert_id, channel_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX notification_state_channel ON notification_state (channel_id);
