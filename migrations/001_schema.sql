-- outofmatrix: self-hosted personal media cloud
-- PostgreSQL 14+ schema

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------------------------------------------------------------------------
-- users
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT        NOT NULL,
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL DEFAULT 'user'
                              CHECK (role IN ('user', 'admin')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Case-insensitive uniqueness on username.
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_username_lower
    ON users (lower(username));

-- ---------------------------------------------------------------------------
-- media_items
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS media_items (
    id             UUID        PRIMARY KEY,
    user_id        UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title          TEXT        NOT NULL DEFAULT '',
    file_path      TEXT        NOT NULL,
    type           TEXT        NOT NULL
                               CHECK (type IN ('photo', 'audio', 'video')),
    status         TEXT        NOT NULL DEFAULT 'pending'
                               CHECK (status IN ('pending', 'processing', 'ready', 'failed')),
    file_size      BIGINT      NOT NULL DEFAULT 0,
    mime_type      TEXT        NOT NULL DEFAULT '',
    blur_hash      TEXT        NOT NULL DEFAULT '',
    thumbnail_path TEXT        NOT NULL DEFAULT '',
    hls_path       TEXT        NOT NULL DEFAULT '',
    metadata       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Primary listing pattern: a user's timeline, newest first.
CREATE INDEX IF NOT EXISTS idx_media_items_user_created
    ON media_items (user_id, created_at DESC, id DESC);

-- Filtered listing pattern: a user's photos / audio / videos, newest first.
CREATE INDEX IF NOT EXISTS idx_media_items_user_type_created
    ON media_items (user_id, type, created_at DESC, id DESC);

-- Partial index for the background pipeline: only unfinished work is indexed,
-- so the index stays tiny no matter how large the library grows.
CREATE INDEX IF NOT EXISTS idx_media_items_unfinished
    ON media_items (status, created_at)
    WHERE status IN ('pending', 'processing');

-- Containment queries against extracted metadata, e.g.
--   metadata @> '{"video_codec": "h264"}'
CREATE INDEX IF NOT EXISTS idx_media_items_metadata_gin
    ON media_items USING GIN (metadata jsonb_path_ops);

-- ---------------------------------------------------------------------------
-- collections (playlists for audio, albums for photos/videos)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS collections (
    id         UUID        PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    type       TEXT        NOT NULL
                           CHECK (type IN ('playlist', 'album')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name, type)
);

CREATE INDEX IF NOT EXISTS idx_collections_user_created
    ON collections (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS collection_items (
    collection_id UUID        NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    media_id      UUID        NOT NULL REFERENCES media_items (id) ON DELETE CASCADE,
    position      INT         NOT NULL DEFAULT 0,
    added_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (collection_id, media_id)
);

-- Reverse lookup: "which collections contain this media item?"
CREATE INDEX IF NOT EXISTS idx_collection_items_media
    ON collection_items (media_id);
