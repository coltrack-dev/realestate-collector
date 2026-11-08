ALTER TABLE listings
    ADD COLUMN IF NOT EXISTS resolved_latitude NUMERIC,
    ADD COLUMN IF NOT EXISTS resolved_longitude NUMERIC,
    ADD COLUMN IF NOT EXISTS location_precision TEXT NOT NULL DEFAULT 'UNKNOWN'
        CHECK (location_precision IN ('EXACT', 'ADDRESS', 'NEIGHBORHOOD', 'CITY', 'DEPARTMENT', 'COUNTRY', 'UNKNOWN')),
    ADD COLUMN IF NOT EXISTS geocode_provider TEXT,
    ADD COLUMN IF NOT EXISTS geocoded_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS listings_location_precision_idx
    ON listings(location_precision);
