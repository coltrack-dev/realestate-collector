package importer

import (
	"context"
	"fmt"
	"time"

	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/parser"
)

// Saver is the persistence boundary for an import. The PostgreSQL store
// implements it while tests can use an in-memory implementation.
type Saver interface {
	Save(context.Context, model.Listing) (created bool, snapshotCreated bool, err error)
}

type Stats struct {
	Read              int
	Accepted          int
	Skipped           int
	Created           int
	Updated           int
	SnapshotsCreated  int
	Errors            int
	StartedAt         time.Time
	FinishedAt        time.Time
}

func (s Stats) Duration() time.Duration { return s.FinishedAt.Sub(s.StartedAt) }

// Import validates and persists listings. In dry-run mode saver may be nil;
// validation and normalization still happen, but no database operation does.
func Import(ctx context.Context, listings []model.Listing, saver Saver, dryRun bool) (Stats, error) {
	stats := Stats{StartedAt: time.Now().UTC()}
	for _, listing := range listings {
		stats.Read++
		if err := parser.Validate(listing); err != nil {
			stats.Skipped++
			stats.Errors++
			continue
		}
		stats.Accepted++
		if dryRun {
			continue
		}
		if saver == nil {
			stats.FinishedAt = time.Now().UTC()
			return stats, fmt.Errorf("listing saver is required unless dry-run is enabled")
		}
		created, snapshotCreated, err := saver.Save(ctx, listing)
		if err != nil {
			stats.FinishedAt = time.Now().UTC()
			return stats, fmt.Errorf("save listing %s/%s: %w", listing.Source, listing.ExternalID, err)
		}
		if created {
			stats.Created++
		} else {
			stats.Updated++
		}
		if snapshotCreated {
			stats.SnapshotsCreated++
		}
	}
	stats.FinishedAt = time.Now().UTC()
	return stats, nil
}
