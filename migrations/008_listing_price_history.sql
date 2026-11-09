CREATE TABLE IF NOT EXISTS listing_price_history (
    id BIGSERIAL PRIMARY KEY,
    listing_id BIGINT NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
    price BIGINT NOT NULL,
    currency TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT listing_price_history_currency_not_blank CHECK (btrim(currency) <> ''),
    CONSTRAINT listing_price_history_event_unique UNIQUE (listing_id, price, currency, observed_at)
);

CREATE INDEX IF NOT EXISTS listing_price_history_listing_observed_idx
    ON listing_price_history(listing_id, observed_at, id);
