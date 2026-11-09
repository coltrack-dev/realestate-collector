package postgres

import (
	"strings"
	"testing"
)

func TestSummaryUpsertPreservesDetailsWhenSummaryIsIncomplete(t *testing.T) {
	for _, fragment := range []string{
		"description = CASE WHEN listings.details_status = 'READY' THEN listings.description WHEN NULLIF(EXCLUDED.description, '') IS NOT NULL THEN EXCLUDED.description ELSE listings.description END",
		"images = CASE WHEN listings.details_status = 'READY' THEN listings.images WHEN jsonb_array_length(EXCLUDED.images) > 0 THEN EXCLUDED.images ELSE listings.images END",
	} {
		if !strings.Contains(upsertListingQuery, fragment) {
			t.Fatalf("summary upsert does not preserve details; missing SQL fragment %q", fragment)
		}
	}
}
