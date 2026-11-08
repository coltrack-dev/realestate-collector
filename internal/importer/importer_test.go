package importer

import (
	"context"
	"testing"
	"time"

	"github.com/example/realestate-collector/internal/model"
)

type fakeSaver struct {
	 calls int
}

func (s *fakeSaver) Save(context.Context, model.Listing) (bool, bool, error) {
	s.calls++
	return s.calls == 1, s.calls == 1, nil
}

func TestImportDryRunDoesNotSave(t *testing.T) {
	saver := &fakeSaver{}
	listings := []model.Listing{
		{Source: "fixture", ExternalID: "1", URL: "https://example.test/1", Operation: "SALE", PropertyType: "APARTMENT", CollectedAt: time.Now()},
		{Source: "fixture", ExternalID: "", URL: "https://example.test/2", Operation: "SALE", PropertyType: "APARTMENT"},
	}
	stats, err := Import(context.Background(), listings, saver, true)
	if err != nil {
		t.Fatal(err)
	}
	if saver.calls != 0 || stats.Read != 2 || stats.Accepted != 1 || stats.Skipped != 1 || stats.Errors != 1 {
		t.Fatalf("unexpected dry-run stats: %#v, calls=%d", stats, saver.calls)
	}
}

func TestImportCountsCreatedAndUpdated(t *testing.T) {
	saver := &fakeSaver{}
	listings := []model.Listing{
		{Source: "fixture", ExternalID: "1", URL: "https://example.test/1", Operation: "SALE", PropertyType: "APARTMENT"},
		{Source: "fixture", ExternalID: "2", URL: "https://example.test/2", Operation: "SALE", PropertyType: "APARTMENT"},
	}
	stats, err := Import(context.Background(), listings, saver, false)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Created != 1 || stats.Updated != 1 || stats.SnapshotsCreated != 1 {
		t.Fatalf("unexpected import stats: %#v", stats)
	}
}
