ALTER TABLE listings
    ADD COLUMN IF NOT EXISTS country_code TEXT;

CREATE INDEX IF NOT EXISTS listings_country_code_idx ON listings(country_code);
