-- Favorites + capture-date sorting.
--
-- captured_at is the moment the media was actually taken/recorded, extracted
-- from container metadata (e.g. QuickTime creation_time) during processing.
-- It is nullable: sorting falls back to created_at when unknown.

ALTER TABLE media_items
    ADD COLUMN IF NOT EXISTS is_favorite boolean NOT NULL DEFAULT false;

ALTER TABLE media_items
    ADD COLUMN IF NOT EXISTS captured_at timestamptz;

-- Favorites are a small subset; a partial index keeps it cheap.
CREATE INDEX IF NOT EXISTS idx_media_items_user_favorite
    ON media_items (user_id, created_at DESC, id DESC)
    WHERE is_favorite;

-- Sort by name (case-insensitive).
CREATE INDEX IF NOT EXISTS idx_media_items_user_title
    ON media_items (user_id, lower(title));

-- Sort by capture date with created_at fallback.
CREATE INDEX IF NOT EXISTS idx_media_items_user_captured
    ON media_items (user_id, (COALESCE(captured_at, created_at)) DESC, id DESC);
