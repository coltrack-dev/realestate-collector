CREATE TABLE IF NOT EXISTS listing_images (
    id BIGSERIAL PRIMARY KEY,
    listing_id BIGINT NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
    position INT NOT NULL CHECK (position >= 0),
    original_url TEXT NOT NULL,
    storage_mode TEXT NOT NULL DEFAULT 'EXTERNAL_URL'
        CHECK (storage_mode IN ('EXTERNAL_URL', 'TEMP_CACHE', 'LOCAL_STORAGE')),
    local_path TEXT,
    content_hash TEXT,
    mime_type TEXT,
    byte_size BIGINT,
    fetched_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(listing_id, position)
);

CREATE INDEX IF NOT EXISTS listing_images_listing_idx ON listing_images(listing_id, position);
CREATE INDEX IF NOT EXISTS listing_images_hash_idx ON listing_images(content_hash);
CREATE INDEX IF NOT EXISTS listing_images_expiry_idx ON listing_images(expires_at)
    WHERE expires_at IS NOT NULL;
