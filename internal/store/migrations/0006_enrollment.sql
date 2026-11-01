-- The codes admins make with Add agent. Each one enrolls the agent on one
-- machine: the first that uses it. That machine can use it again, such as
-- after a failed install, until it expires.
CREATE TABLE enrollment_codes (
    code_hash  BLOB PRIMARY KEY,  -- SHA-256 of the code
    created_by TEXT NOT NULL,     -- the login of the admin who made it
    created_at INTEGER NOT NULL,  -- Unix milliseconds
    expires_at INTEGER NOT NULL,  -- Unix milliseconds
    node_id    TEXT               -- the node that used it, or NULL
) STRICT;

-- The secret of each enrolled agent, by the Tailscale node ID of the machine
-- it runs on. Bield takes the agent's heartbeats only from that node.
CREATE TABLE enrollments (
    node_id     TEXT PRIMARY KEY,
    secret_hash BLOB NOT NULL UNIQUE, -- SHA-256 of the secret
    enrolled_at INTEGER NOT NULL,     -- Unix milliseconds
    enrolled_by TEXT NOT NULL         -- the admin who made the code it used
) STRICT;

-- Agents used to share one token (#24). Each has its own secret now.
DELETE FROM kv WHERE key = 'agent_token';
