-- The Tailscale logins that have a role in Bield, admin or viewer. Each login
-- has one role. Tailscale doesn't promise a login's case, so they're compared
-- without it. This replaces the admins table.
CREATE TABLE roles (
    login      TEXT PRIMARY KEY COLLATE NOCASE,
    role       TEXT NOT NULL CHECK (role IN ('admin', 'viewer')),
    granted_at INTEGER NOT NULL -- Unix milliseconds, when the login got this role
) STRICT;

INSERT INTO roles (login, role, granted_at) SELECT login, 'admin', added_at FROM admins;

DROP TABLE admins;
