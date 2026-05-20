CREATE TABLE IF NOT EXISTS users (
    id            SERIAL      PRIMARY KEY,
    username      TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL DEFAULT 'viewer'
                  CHECK (role IN ('admin', 'editor', 'reviewer', 'viewer')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT        PRIMARY KEY,
    user_id    INTEGER     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id  ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires  ON sessions (expires_at);

ALTER TABLE policies
    ADD COLUMN IF NOT EXISTS status          TEXT        NOT NULL DEFAULT 'active'
                              CHECK (status IN ('active', 'pending_review', 'rejected', 'draft')),
    ADD COLUMN IF NOT EXISTS submitted_by    TEXT,
    ADD COLUMN IF NOT EXISTS reviewed_by     TEXT,
    ADD COLUMN IF NOT EXISTS review_comment  TEXT,
    ADD COLUMN IF NOT EXISTS submitted_at    TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_policies_status ON policies (status) WHERE status <> 'active';
