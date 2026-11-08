package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/normalize"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ Pool *pgxpool.Pool }

const beginRunQuery = `
WITH source AS (
    INSERT INTO data_sources(name, base_url)
    VALUES ($1, $1)
    ON CONFLICT(name) DO UPDATE SET name = EXCLUDED.name
    RETURNING id
)
INSERT INTO collection_runs(source_id, started_at, status)
SELECT id, $2, 'RUNNING'
FROM source
RETURNING id`

const finishRunQuery = `
UPDATE collection_runs
SET finished_at = now(),
    status = $2,
    listings_found = $3,
    listings_created = $4,
    listings_updated = $5,
    error_message = $6
WHERE id = $1`

const saveTransactionQuery = `
INSERT INTO property_transactions(
    source, external_id, matricula, old_cadastral_number,
    current_cadastral_number, municipality, department, divipola, orip,
    filing_date_text, opening_date_text, date_text, year, property_zone, rurality,
    annotation_number, folio_status, derived_folios, new_properties,
    has_multiple_value, transaction_value, has_value, count_a, count_de,
    justifying_document, legal_act_code, legal_act_name, dynamic, collected_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
    $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27,
    $28, $29
)
ON CONFLICT(source, external_id) DO UPDATE SET
    matricula = EXCLUDED.matricula,
    old_cadastral_number = EXCLUDED.old_cadastral_number,
    current_cadastral_number = EXCLUDED.current_cadastral_number,
    municipality = EXCLUDED.municipality,
    department = EXCLUDED.department,
    divipola = EXCLUDED.divipola,
    orip = EXCLUDED.orip,
    filing_date_text = EXCLUDED.filing_date_text,
    opening_date_text = EXCLUDED.opening_date_text,
    date_text = EXCLUDED.date_text,
    year = EXCLUDED.year,
    property_zone = EXCLUDED.property_zone,
    rurality = EXCLUDED.rurality,
    annotation_number = EXCLUDED.annotation_number,
    folio_status = EXCLUDED.folio_status,
    derived_folios = EXCLUDED.derived_folios,
    new_properties = EXCLUDED.new_properties,
    has_multiple_value = EXCLUDED.has_multiple_value,
    transaction_value = EXCLUDED.transaction_value,
    has_value = EXCLUDED.has_value,
    count_a = EXCLUDED.count_a,
    count_de = EXCLUDED.count_de,
    justifying_document = EXCLUDED.justifying_document,
    legal_act_code = EXCLUDED.legal_act_code,
    legal_act_name = EXCLUDED.legal_act_name,
    dynamic = EXCLUDED.dynamic,
    collected_at = EXCLUDED.collected_at,
    updated_at = now()
RETURNING (xmax = 0)`

const upsertSourceQuery = `
INSERT INTO data_sources(name, base_url)
VALUES ($1, $2)
ON CONFLICT(name) DO UPDATE SET base_url = EXCLUDED.base_url
RETURNING id`

const upsertListingQuery = `
INSERT INTO listings(
    source_id, external_id, url, title, description, operation,
    property_type, price, currency, area_m2, bedrooms, bathrooms,
    parking_spots, country, country_code, department, city, neighborhood, address,
    latitude, longitude, images, published_at, first_seen_at,
    last_seen_at, updated_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
    $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $24, $24
)
ON CONFLICT(source_id, external_id) DO UPDATE SET
    url = EXCLUDED.url,
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    operation = EXCLUDED.operation,
    property_type = EXCLUDED.property_type,
    price = COALESCE(EXCLUDED.price, listings.price),
    currency = COALESCE(NULLIF(EXCLUDED.currency, ''), listings.currency),
    area_m2 = COALESCE(EXCLUDED.area_m2, listings.area_m2),
    bedrooms = COALESCE(EXCLUDED.bedrooms, listings.bedrooms),
    bathrooms = COALESCE(EXCLUDED.bathrooms, listings.bathrooms),
    parking_spots = COALESCE(EXCLUDED.parking_spots, listings.parking_spots),
    country = COALESCE(NULLIF(EXCLUDED.country, ''), listings.country),
    country_code = COALESCE(NULLIF(EXCLUDED.country_code, ''), listings.country_code),
    department = COALESCE(NULLIF(EXCLUDED.department, ''), listings.department),
    city = EXCLUDED.city,
    neighborhood = EXCLUDED.neighborhood,
    address = COALESCE(NULLIF(EXCLUDED.address, ''), listings.address),
    latitude = COALESCE(EXCLUDED.latitude, listings.latitude),
    longitude = COALESCE(EXCLUDED.longitude, listings.longitude),
    resolved_latitude = CASE WHEN
        COALESCE(NULLIF(EXCLUDED.department, ''), listings.department) IS DISTINCT FROM listings.department OR
        COALESCE(NULLIF(EXCLUDED.city, ''), listings.city) IS DISTINCT FROM listings.city OR
        COALESCE(NULLIF(EXCLUDED.neighborhood, ''), listings.neighborhood) IS DISTINCT FROM listings.neighborhood OR
        COALESCE(NULLIF(EXCLUDED.address, ''), listings.address) IS DISTINCT FROM listings.address OR
        COALESCE(NULLIF(EXCLUDED.country_code, ''), listings.country_code) IS DISTINCT FROM listings.country_code
        THEN NULL ELSE listings.resolved_latitude END,
    resolved_longitude = CASE WHEN
        COALESCE(NULLIF(EXCLUDED.department, ''), listings.department) IS DISTINCT FROM listings.department OR
        COALESCE(NULLIF(EXCLUDED.city, ''), listings.city) IS DISTINCT FROM listings.city OR
        COALESCE(NULLIF(EXCLUDED.neighborhood, ''), listings.neighborhood) IS DISTINCT FROM listings.neighborhood OR
        COALESCE(NULLIF(EXCLUDED.address, ''), listings.address) IS DISTINCT FROM listings.address OR
        COALESCE(NULLIF(EXCLUDED.country_code, ''), listings.country_code) IS DISTINCT FROM listings.country_code
        THEN NULL ELSE listings.resolved_longitude END,
    location_precision = CASE WHEN
        COALESCE(NULLIF(EXCLUDED.department, ''), listings.department) IS DISTINCT FROM listings.department OR
        COALESCE(NULLIF(EXCLUDED.city, ''), listings.city) IS DISTINCT FROM listings.city OR
        COALESCE(NULLIF(EXCLUDED.neighborhood, ''), listings.neighborhood) IS DISTINCT FROM listings.neighborhood OR
        COALESCE(NULLIF(EXCLUDED.address, ''), listings.address) IS DISTINCT FROM listings.address OR
        COALESCE(NULLIF(EXCLUDED.country_code, ''), listings.country_code) IS DISTINCT FROM listings.country_code
        THEN 'UNKNOWN' ELSE listings.location_precision END,
    geocode_provider = CASE WHEN
        COALESCE(NULLIF(EXCLUDED.department, ''), listings.department) IS DISTINCT FROM listings.department OR
        COALESCE(NULLIF(EXCLUDED.city, ''), listings.city) IS DISTINCT FROM listings.city OR
        COALESCE(NULLIF(EXCLUDED.neighborhood, ''), listings.neighborhood) IS DISTINCT FROM listings.neighborhood OR
        COALESCE(NULLIF(EXCLUDED.address, ''), listings.address) IS DISTINCT FROM listings.address OR
        COALESCE(NULLIF(EXCLUDED.country_code, ''), listings.country_code) IS DISTINCT FROM listings.country_code
        THEN NULL ELSE listings.geocode_provider END,
    geocoded_at = CASE WHEN
        COALESCE(NULLIF(EXCLUDED.department, ''), listings.department) IS DISTINCT FROM listings.department OR
        COALESCE(NULLIF(EXCLUDED.city, ''), listings.city) IS DISTINCT FROM listings.city OR
        COALESCE(NULLIF(EXCLUDED.neighborhood, ''), listings.neighborhood) IS DISTINCT FROM listings.neighborhood OR
        COALESCE(NULLIF(EXCLUDED.address, ''), listings.address) IS DISTINCT FROM listings.address OR
        COALESCE(NULLIF(EXCLUDED.country_code, ''), listings.country_code) IS DISTINCT FROM listings.country_code
        THEN NULL ELSE listings.geocoded_at END,
    images = EXCLUDED.images,
    published_at = EXCLUDED.published_at,
    last_seen_at = EXCLUDED.last_seen_at,
    updated_at = EXCLUDED.updated_at
RETURNING id, (xmax = 0)`

const insertSnapshotQuery = `
INSERT INTO listing_snapshots(
    listing_id, price, currency, area_m2, content_hash, collected_at
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT(listing_id, content_hash) DO NOTHING
RETURNING id`

func (s *Store) BeginRun(ctx context.Context, source string, started time.Time) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, beginRunQuery, source, started).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("begin collection run: %w", err)
	}
	return id, err
}

func (s *Store) FinishRun(ctx context.Context, id int64, status string, found, created, updated int, runErr error) error {
	var message any
	if runErr != nil {
		message = runErr.Error()
	}
	_, err := s.Pool.Exec(ctx, finishRunQuery, id, status, found, created, updated, message)
	if err != nil {
		return fmt.Errorf("finish collection run: %w", err)
	}
	return nil
}

func (s *Store) SaveTransaction(ctx context.Context, t model.PropertyTransaction) (bool, error) {
	var created bool
	err := s.Pool.QueryRow(ctx, saveTransactionQuery,
		t.Source, t.ExternalID, t.Matricula, t.OldCadastralNumber,
		t.CurrentCadastralNumber, t.Municipality, t.Department, t.Divipola,
		t.ORIP, t.FilingDateText, t.OpeningDateText, t.FilingDateText, t.Year, t.PropertyZone,
		t.Rurality, t.AnnotationNumber, t.FolioStatus, t.DerivedFolios,
		t.NewProperties, t.HasMultipleValue, t.Value, t.HasValue, t.CountA,
		t.CountDE, t.JustifyingDocument, t.LegalActCode, t.LegalActName,
		t.Dynamic, t.CollectedAt,
	).Scan(&created)
	if err != nil {
		return false, fmt.Errorf("save property transaction %s: %w", t.ExternalID, err)
	}
	return created, nil
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", e)
	}
	if e = p.Ping(ctx); e != nil {
		p.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", e)
	}
	return &Store{Pool: p}, nil
}
func (s *Store) Close() { s.Pool.Close() }
func (s *Store) Save(ctx context.Context, l model.Listing) (created, updated bool, err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, false, fmt.Errorf("begin listing transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	sourceID, err := upsertSource(ctx, tx, l)
	if err != nil {
		return false, false, err
	}
	now := l.CollectedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	listingID, created, err := upsertListing(ctx, tx, sourceID, l, now)
	if err != nil {
		return false, false, err
	}
	updated, err = insertSnapshot(ctx, tx, listingID, l, now)
	if err != nil {
		return false, false, err
	}
	if err = syncListingImages(ctx, tx, listingID, l.Images); err != nil {
		return false, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, false, fmt.Errorf("commit listing transaction: %w", err)
	}
	return created, updated, nil
}

func (s *Store) SetDetailsStatus(ctx context.Context, listingID int64, status string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE listings SET details_status = $2 WHERE id = $1`, listingID, status)
	if err != nil {
		return fmt.Errorf("set details status for listing %d: %w", listingID, err)
	}
	return nil
}

// SaveGeocode stores a resolved location separately from source coordinates.
// It never changes latitude or longitude received from the source.
func (s *Store) SaveGeocode(ctx context.Context, listingID int64, latitude, longitude *float64, precision, provider string, geocodedAt time.Time) error {
	_, err := s.Pool.Exec(ctx, `
UPDATE listings
SET resolved_latitude = $2,
    resolved_longitude = $3,
    location_precision = $4,
    geocode_provider = NULLIF($5, ''),
    geocoded_at = $6
WHERE id = $1`, listingID, latitude, longitude, precision, provider, geocodedAt)
	if err != nil {
		return fmt.Errorf("save geocode for listing %d: %w", listingID, err)
	}
	return nil
}

func (s *Store) UpdateDetails(ctx context.Context, listingID int64, details model.Listing, fetchedAt, expiresAt time.Time) error {
	images, err := json.Marshal(details.Images)
	if err != nil {
		return fmt.Errorf("encode details images for listing %d: %w", listingID, err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin details transaction for listing %d: %w", listingID, err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
UPDATE listings
SET description = COALESCE(NULLIF($2, ''), description),
    area_m2 = COALESCE($3, area_m2),
    bedrooms = COALESCE($4, bedrooms),
    bathrooms = COALESCE($5, bathrooms),
    parking_spots = COALESCE($6, parking_spots),
    department = COALESCE(NULLIF($7, ''), department),
    neighborhood = COALESCE(NULLIF($8, ''), neighborhood),
    address = COALESCE(NULLIF($9, ''), address),
    resolved_latitude = CASE WHEN
        COALESCE(NULLIF($7, ''), department) IS DISTINCT FROM department OR
        COALESCE(NULLIF($8, ''), neighborhood) IS DISTINCT FROM neighborhood OR
        COALESCE(NULLIF($9, ''), address) IS DISTINCT FROM address
        THEN NULL ELSE resolved_latitude END,
    resolved_longitude = CASE WHEN
        COALESCE(NULLIF($7, ''), department) IS DISTINCT FROM department OR
        COALESCE(NULLIF($8, ''), neighborhood) IS DISTINCT FROM neighborhood OR
        COALESCE(NULLIF($9, ''), address) IS DISTINCT FROM address
        THEN NULL ELSE resolved_longitude END,
    location_precision = CASE WHEN
        COALESCE(NULLIF($7, ''), department) IS DISTINCT FROM department OR
        COALESCE(NULLIF($8, ''), neighborhood) IS DISTINCT FROM neighborhood OR
        COALESCE(NULLIF($9, ''), address) IS DISTINCT FROM address
        THEN 'UNKNOWN' ELSE location_precision END,
    geocode_provider = CASE WHEN
        COALESCE(NULLIF($7, ''), department) IS DISTINCT FROM department OR
        COALESCE(NULLIF($8, ''), neighborhood) IS DISTINCT FROM neighborhood OR
        COALESCE(NULLIF($9, ''), address) IS DISTINCT FROM address
        THEN NULL ELSE geocode_provider END,
    geocoded_at = CASE WHEN
        COALESCE(NULLIF($7, ''), department) IS DISTINCT FROM department OR
        COALESCE(NULLIF($8, ''), neighborhood) IS DISTINCT FROM neighborhood OR
        COALESCE(NULLIF($9, ''), address) IS DISTINCT FROM address
        THEN NULL ELSE geocoded_at END,
    images = CASE WHEN jsonb_array_length($10::jsonb) > 0 THEN $10::jsonb ELSE images END,
    details_fetched_at = $11,
    details_expires_at = $12,
    details_status = 'READY'
WHERE id = $1`, listingID, details.Description, details.AreaM2, details.Bedrooms,
		details.Bathrooms, details.ParkingSpots, details.Department, details.Neighborhood,
		details.Address, images, fetchedAt, expiresAt)
	if err != nil {
		return fmt.Errorf("update details for listing %d: %w", listingID, err)
	}
	if len(details.Images) > 0 {
		if err := syncListingImages(ctx, tx, listingID, details.Images); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit details transaction for listing %d: %w", listingID, err)
	}
	return nil
}

type databaseTransaction interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func upsertSource(ctx context.Context, tx databaseTransaction, listing model.Listing) (int64, error) {
	var sourceID int64
	err := tx.QueryRow(ctx, upsertSourceQuery, listing.Source, listing.URL).Scan(&sourceID)
	if err != nil {
		return 0, fmt.Errorf("upsert data source %s: %w", listing.Source, err)
	}
	return sourceID, nil
}

func upsertListing(ctx context.Context, tx databaseTransaction, sourceID int64, listing model.Listing, collectedAt time.Time) (int64, bool, error) {
	images, err := json.Marshal(listing.Images)
	if err != nil {
		return 0, false, fmt.Errorf("encode images for listing %s: %w", listing.ExternalID, err)
	}
	if listing.Images == nil {
		images = []byte("[]")
	}
	var listingID int64
	var created bool
	err = tx.QueryRow(ctx, upsertListingQuery,
		sourceID, listing.ExternalID, listing.URL, listing.Title,
		listing.Description, listing.Operation, listing.PropertyType,
		listing.Price, listing.Currency, listing.AreaM2, listing.Bedrooms,
		listing.Bathrooms, listing.ParkingSpots, listing.Country,
		listing.CountryCode, listing.Department, listing.City, listing.Neighborhood, listing.Address,
		listing.Latitude, listing.Longitude, images, listing.PublishedAt,
		collectedAt,
	).Scan(&listingID, &created)
	if err != nil {
		return 0, false, fmt.Errorf("upsert listing %s: %w", listing.ExternalID, err)
	}
	return listingID, created, nil
}

func insertSnapshot(ctx context.Context, tx databaseTransaction, listingID int64, listing model.Listing, collectedAt time.Time) (bool, error) {
	sum := sha256.Sum256([]byte(normalize.Hashable(listing)))
	hash := hex.EncodeToString(sum[:])
	var snapshotID int64
	err := tx.QueryRow(ctx, insertSnapshotQuery,
		listingID, listing.Price, listing.Currency, listing.AreaM2, hash, collectedAt,
	).Scan(&snapshotID)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert snapshot for listing %s: %w", listing.ExternalID, err)
	}
	return snapshotID != 0, nil
}

func syncListingImages(ctx context.Context, tx databaseTransaction, listingID int64, urls []string) error {
	const query = `
INSERT INTO listing_images(listing_id, position, original_url, storage_mode, updated_at)
VALUES ($1, $2, $3, 'EXTERNAL_URL', now())
ON CONFLICT(listing_id, position) DO UPDATE SET
    original_url = EXCLUDED.original_url,
    storage_mode = 'EXTERNAL_URL',
    updated_at = now(),
    revoked_at = NULL`
	for position, originalURL := range urls {
		if _, err := tx.Exec(ctx, query, listingID, position, originalURL); err != nil {
			return fmt.Errorf("sync image %d for listing %d: %w", position, listingID, err)
		}
	}
	return nil
}
func ApplyMigration(ctx context.Context, p *pgxpool.Pool, path string) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	_, e = p.Exec(ctx, string(b))
	if e != nil {
		return fmt.Errorf("apply migration %s: %w", path, e)
	}
	return nil
}
