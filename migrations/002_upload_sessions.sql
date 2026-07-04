-- Resumable chunked uploads: session bookkeeping + per-chunk receipts.
-- Chunk bytes are written directly into a sparse .part file at
-- chunk_index * chunk_size, so a 5 GB upload never occupies RAM and can be
-- resumed after a network drop or a server restart.

CREATE TABLE IF NOT EXISTS upload_sessions (
    id           UUID        PRIMARY KEY,
    user_id      UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    filename     TEXT        NOT NULL,
    title        TEXT        NOT NULL DEFAULT '',
    mime_type    TEXT        NOT NULL DEFAULT '',
    total_size   BIGINT      NOT NULL CHECK (total_size > 0),
    chunk_size   BIGINT      NOT NULL CHECK (chunk_size > 0),
    total_chunks INT         NOT NULL CHECK (total_chunks > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_upload_sessions_expires
    ON upload_sessions (expires_at);

CREATE INDEX IF NOT EXISTS idx_upload_sessions_user
    ON upload_sessions (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS upload_chunks (
    session_id  UUID        NOT NULL REFERENCES upload_sessions (id) ON DELETE CASCADE,
    chunk_index INT         NOT NULL,
    size        BIGINT      NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, chunk_index)
);
