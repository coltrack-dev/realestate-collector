ALTER TABLE property_transactions
    ADD COLUMN IF NOT EXISTS old_cadastral_number TEXT,
    ADD COLUMN IF NOT EXISTS current_cadastral_number TEXT,
    ADD COLUMN IF NOT EXISTS filing_date_text TEXT,
    ADD COLUMN IF NOT EXISTS opening_date_text TEXT,
    ADD COLUMN IF NOT EXISTS rurality TEXT,
    ADD COLUMN IF NOT EXISTS annotation_number INT,
    ADD COLUMN IF NOT EXISTS folio_status TEXT,
    ADD COLUMN IF NOT EXISTS derived_folios TEXT,
    ADD COLUMN IF NOT EXISTS new_properties INT,
    ADD COLUMN IF NOT EXISTS has_multiple_value BOOLEAN,
    ADD COLUMN IF NOT EXISTS count_a INT,
    ADD COLUMN IF NOT EXISTS count_de INT,
    ADD COLUMN IF NOT EXISTS justifying_document TEXT,
    ADD COLUMN IF NOT EXISTS legal_act_code TEXT,
    ADD COLUMN IF NOT EXISTS legal_act_name TEXT;

COMMENT ON TABLE property_transactions IS
    'IGAC/SNR registered transactions; values are transaction values, not listing prices.';
