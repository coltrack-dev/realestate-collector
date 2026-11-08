package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/example/realestate-collector/internal/api"
)

type ReadRepository struct{ store *Store }

func NewReadRepository(store *Store) *ReadRepository { return &ReadRepository{store: store} }

func (r *ReadRepository) Ping(ctx context.Context) error { return r.store.Pool.Ping(ctx) }

func (r *ReadRepository) List(ctx context.Context, filter api.ListingFilter) ([]api.ListingRecord, int64, error) {
	conditions, args := listingConditions(filter)
	where := ""
	if len(conditions) > 0 { where = "WHERE " + strings.Join(conditions, " AND ") }
	args = append(args, filter.Limit, filter.Offset)
	query := fmt.Sprintf(`
SELECT l.id, ds.name, l.external_id, l.url, l.title, l.description,
       l.operation, l.property_type, l.price, l.currency, l.area_m2,
       l.bedrooms, l.bathrooms, l.parking_spots, l.country, l.department,
       l.city, l.neighborhood, l.address, l.latitude, l.longitude, l.images,
       l.published_at, l.first_seen_at, l.last_seen_at, count(*) OVER() AS total_count
FROM listings l
JOIN data_sources ds ON ds.id = l.source_id
%s
ORDER BY l.updated_at DESC, l.id DESC
LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))
	rows, err := r.store.Pool.Query(ctx, query, args...)
	if err != nil { return nil, 0, fmt.Errorf("list listings: %w", err) }
	defer rows.Close()
	result := make([]api.ListingRecord, 0, filter.Limit)
	var total int64
	for rows.Next() {
		record, rowTotal, err := scanListing(rows)
		if err != nil { return nil, 0, fmt.Errorf("scan listing: %w", err) }
		result = append(result, record)
		total = rowTotal
	}
	if err := rows.Err(); err != nil { return nil, 0, fmt.Errorf("iterate listings: %w", err) }
	return result, total, nil
}

func (r *ReadRepository) Get(ctx context.Context, id int64) (api.ListingRecord, error) {
	rows, err := r.store.Pool.Query(ctx, `
SELECT l.id, ds.name, l.external_id, l.url, l.title, l.description,
       l.operation, l.property_type, l.price, l.currency, l.area_m2,
       l.bedrooms, l.bathrooms, l.parking_spots, l.country, l.department,
       l.city, l.neighborhood, l.address, l.latitude, l.longitude, l.images,
       l.published_at, l.first_seen_at, l.last_seen_at, 1::bigint
FROM listings l
JOIN data_sources ds ON ds.id = l.source_id
WHERE l.id = $1`, id)
	if err != nil { return api.ListingRecord{}, fmt.Errorf("get listing %d: %w", id, err) }
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil { return api.ListingRecord{}, fmt.Errorf("read listing %d: %w", id, err) }
		return api.ListingRecord{}, api.ErrNotFound
	}
	record, _, err := scanListing(rows)
	if err != nil { return api.ListingRecord{}, fmt.Errorf("scan listing %d: %w", id, err) }
	return record, nil
}

func (r *ReadRepository) History(ctx context.Context, listingID int64) ([]api.Snapshot, error) {
	rows, err := r.store.Pool.Query(ctx, `
SELECT id, price, currency, area_m2, content_hash, collected_at
FROM listing_snapshots
WHERE listing_id = $1
ORDER BY collected_at DESC, id DESC`, listingID)
	if err != nil { return nil, fmt.Errorf("list snapshots for %d: %w", listingID, err) }
	defer rows.Close()
	result := make([]api.Snapshot, 0)
	for rows.Next() {
		var snapshot api.Snapshot
		if err := rows.Scan(&snapshot.ID, &snapshot.Price, &snapshot.Currency, &snapshot.AreaM2, &snapshot.ContentHash, &snapshot.CollectedAt); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		result = append(result, snapshot)
	}
	if err := rows.Err(); err != nil { return nil, fmt.Errorf("iterate snapshots: %w", err) }
	return result, nil
}

func (r *ReadRepository) Stats(ctx context.Context) (api.Stats, error) {
	var stats api.Stats
	if err := r.store.Pool.QueryRow(ctx, `SELECT count(*) FROM listings`).Scan(&stats.Total); err != nil { return stats, fmt.Errorf("count listings: %w", err) }
	stats.BySource = make(map[string]int64)
	rows, err := r.store.Pool.Query(ctx, `SELECT ds.name, count(*) FROM listings l JOIN data_sources ds ON ds.id=l.source_id GROUP BY ds.name ORDER BY ds.name`)
	if err != nil { return stats, fmt.Errorf("count listings by source: %w", err) }
	for rows.Next() { var name string; var count int64; if err := rows.Scan(&name, &count); err != nil { rows.Close(); return stats, err }; stats.BySource[name] = count }
	if err := rows.Err(); err != nil { rows.Close(); return stats, err }; rows.Close()
	stats.ByOperation = make(map[string]int64)
	rows, err = r.store.Pool.Query(ctx, `SELECT operation, count(*) FROM listings GROUP BY operation ORDER BY operation`)
	if err != nil { return stats, fmt.Errorf("count listings by operation: %w", err) }
	for rows.Next() { var operation string; var count int64; if err := rows.Scan(&operation, &count); err != nil { rows.Close(); return stats, err }; stats.ByOperation[operation] = count }
	if err := rows.Err(); err != nil { rows.Close(); return stats, err }; rows.Close()
	if err := r.store.Pool.QueryRow(ctx, `SELECT max(updated_at) FROM listings`).Scan(&stats.LastUpdatedAt); err != nil { return stats, fmt.Errorf("find last listing update: %w", err) }
	return stats, nil
}

func listingConditions(filter api.ListingFilter) ([]string, []any) {
	conditions := make([]string, 0, 8)
	args := make([]any, 0, 8)
	add := func(condition string, value any) { args = append(args, value); conditions = append(conditions, fmt.Sprintf(condition, len(args))) }
	if filter.Source != "" { add("ds.name = $%d", filter.Source) }
	if filter.City != "" { add("l.city = $%d", filter.City) }
	if filter.Operation != "" { add("l.operation = $%d", filter.Operation) }
	if filter.PropertyType != "" { add("l.property_type = $%d", filter.PropertyType) }
	if filter.MinPrice != nil { add("l.price >= $%d", *filter.MinPrice) }
	if filter.MaxPrice != nil { add("l.price <= $%d", *filter.MaxPrice) }
	if filter.MinArea != nil { add("l.area_m2 >= $%d", *filter.MinArea) }
	if filter.MaxArea != nil { add("l.area_m2 <= $%d", *filter.MaxArea) }
	return conditions, args
}

type listingRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanListing(rows listingRows) (api.ListingRecord, int64, error) {
	var record api.ListingRecord
	var images []byte
	var lastSeen time.Time
	var firstSeen time.Time
	var total int64
	var currency, country, department, city, neighborhood, address *string
	err := rows.Scan(
		&record.ID, &record.Listing.Source, &record.Listing.ExternalID, &record.Listing.URL,
		&record.Listing.Title, &record.Listing.Description, &record.Listing.Operation,
		&record.Listing.PropertyType, &record.Listing.Price, &currency,
		&record.Listing.AreaM2, &record.Listing.Bedrooms, &record.Listing.Bathrooms,
		&record.Listing.ParkingSpots, &country, &department,
		&city, &neighborhood, &address,
		&record.Listing.Latitude, &record.Listing.Longitude, &images, &record.Listing.PublishedAt,
		&firstSeen, &lastSeen, &total,
	)
	if err != nil { return api.ListingRecord{}, 0, err }
	if currency != nil { record.Listing.Currency = *currency }
	if country != nil { record.Listing.Country = *country }
	if department != nil { record.Listing.Department = *department }
	if city != nil { record.Listing.City = *city }
	if neighborhood != nil { record.Listing.Neighborhood = *neighborhood }
	if address != nil { record.Listing.Address = *address }
	record.Listing.CollectedAt = lastSeen
	record.FirstSeenAt = firstSeen
	record.LastSeenAt = lastSeen
	if len(images) > 0 && string(images) != "null" {
		if err := json.Unmarshal(images, &record.Listing.Images); err != nil { return api.ListingRecord{}, 0, fmt.Errorf("decode images: %w", err) }
	}
	return record, total, nil
}
