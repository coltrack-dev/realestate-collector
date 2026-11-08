ALTER TABLE listings
    ADD COLUMN IF NOT EXISTS details_fetched_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS details_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS details_status TEXT NOT NULL DEFAULT 'NOT_LOADED'
        CHECK (details_status IN ('NOT_LOADED', 'LOADING', 'READY', 'FAILED', 'UNSUPPORTED'));

CREATE INDEX IF NOT EXISTS listings_details_expiry_idx
    ON listings(details_expires_at)
    WHERE details_expires_at IS NOT NULL;
