CREATE TABLE IF NOT EXISTS data_sources (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    base_url TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS listings (
    id BIGSERIAL PRIMARY KEY,
    source_id BIGINT NOT NULL REFERENCES data_sources(id),
    external_id TEXT NOT NULL,
    url TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    operation TEXT NOT NULL,
    property_type TEXT NOT NULL,
    price BIGINT,
    currency TEXT,
    area_m2 NUMERIC,
    bedrooms INT,
    bathrooms INT,
    parking_spots INT,
    country TEXT,
    department TEXT,
    city TEXT,
    neighborhood TEXT,
    address TEXT,
    latitude NUMERIC,
    longitude NUMERIC,
    images JSONB NOT NULL DEFAULT '[]',
    published_at TIMESTAMPTZ,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(source_id, external_id)
);

CREATE TABLE IF NOT EXISTS listing_snapshots (
    id BIGSERIAL PRIMARY KEY,
    listing_id BIGINT NOT NULL REFERENCES listings(id),
    price BIGINT,
    currency TEXT,
    area_m2 NUMERIC,
    content_hash TEXT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    UNIQUE(listing_id, content_hash)
);

CREATE TABLE IF NOT EXISTS collection_runs (
    id BIGSERIAL PRIMARY KEY,
    source_id BIGINT NOT NULL REFERENCES data_sources(id),
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    status TEXT NOT NULL,
    pages_fetched INT NOT NULL DEFAULT 0,
    listings_found INT NOT NULL DEFAULT 0,
    listings_created INT NOT NULL DEFAULT 0,
    listings_updated INT NOT NULL DEFAULT 0,
    error_message TEXT
);

CREATE TABLE IF NOT EXISTS property_transactions (
    id BIGSERIAL PRIMARY KEY,
    source TEXT NOT NULL,
    external_id TEXT NOT NULL,
    matricula TEXT,
    municipality TEXT,
    department TEXT,
    divipola TEXT,
    orip TEXT,
    year TEXT,
    date_text TEXT,
    property_zone TEXT,
    legal_act TEXT,
    transaction_value BIGINT,
    has_value BOOLEAN,
    dynamic INT,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(source, external_id)
);

CREATE INDEX IF NOT EXISTS listings_city_idx ON listings(city);
CREATE INDEX IF NOT EXISTS listings_price_idx ON listings(price);
CREATE INDEX IF NOT EXISTS listings_updated_idx ON listings(updated_at);
CREATE INDEX IF NOT EXISTS snapshots_listing_idx ON listing_snapshots(listing_id, collected_at);
CREATE INDEX IF NOT EXISTS property_transactions_municipality_idx
    ON property_transactions(municipality);
CREATE INDEX IF NOT EXISTS property_transactions_year_idx
    ON property_transactions(year);
CREATE INDEX IF NOT EXISTS property_transactions_collected_idx
    ON property_transactions(collected_at);
