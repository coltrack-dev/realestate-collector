package postgres

import (
	"testing"

	"github.com/example/realestate-collector/internal/model"
)

func TestShouldRecordPriceHistory(t *testing.T) {
	price := int64(100)
	other := int64(200)
	tests := []struct {
		name             string
		previousPrice    *int64
		previousCurrency string
		listing          model.Listing
		want             bool
	}{
		{"first observation", nil, "", model.Listing{Price: &price, Currency: "COP"}, true},
		{"unchanged", &price, "COP", model.Listing{Price: &price, Currency: "cop"}, false},
		{"price changed", &price, "COP", model.Listing{Price: &other, Currency: "COP"}, true},
		{"currency changed", &price, "COP", model.Listing{Price: &price, Currency: "UYU"}, true},
		{"return to previous price", &other, "COP", model.Listing{Price: &price, Currency: "COP"}, true},
		{"missing price", nil, "", model.Listing{Currency: "COP"}, false},
		{"missing currency", nil, "", model.Listing{Price: &price}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRecordPriceHistory(tt.previousPrice, tt.previousCurrency, tt.listing); got != tt.want {
				t.Fatalf("shouldRecordPriceHistory() = %v, want %v", got, tt.want)
			}
		})
	}
}
