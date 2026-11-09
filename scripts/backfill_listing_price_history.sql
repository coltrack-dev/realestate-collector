-- Run only after 008_listing_price_history.sql has been reviewed and applied.
-- This is idempotent and uses snapshot collection times as observation times.
WITH valid_snapshots AS (
    SELECT id, listing_id, price, btrim(currency) AS currency, collected_at
    FROM listing_snapshots
    WHERE price IS NOT NULL AND NULLIF(btrim(currency), '') IS NOT NULL
),
changes AS (
    SELECT listing_id, price, currency, collected_at,
           LAG(price) OVER (PARTITION BY listing_id ORDER BY collected_at, id) AS previous_price,
           LAG(currency) OVER (PARTITION BY listing_id ORDER BY collected_at, id) AS previous_currency
    FROM valid_snapshots
)
INSERT INTO listing_price_history(listing_id, price, currency, observed_at)
SELECT listing_id, price, currency, collected_at
FROM changes
WHERE previous_price IS NULL
   OR price IS DISTINCT FROM previous_price
   OR currency IS DISTINCT FROM previous_currency
ON CONFLICT (listing_id, price, currency, observed_at) DO NOTHING;
