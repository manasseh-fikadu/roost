-- The Tailscale logins of Roost's admins, such as alice@example.com. Tailscale
-- doesn't promise a login's case, so they're compared without it.
CREATE TABLE admins (
    login    TEXT PRIMARY KEY COLLATE NOCASE,
    added_at INTEGER NOT NULL -- Unix milliseconds
) STRICT;
