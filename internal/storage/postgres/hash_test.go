package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/normalize"
	"github.com/example/realestate-collector/internal/parser"
)

func TestContentHashStable(t *testing.T) {
	l := model.Listing{Source: "x", ExternalID: "1", Title: "A"}
	a := sha256.Sum256([]byte(normalize.Hashable(l)))
	b := sha256.Sum256([]byte(normalize.Hashable(l)))
	if hex.EncodeToString(a[:]) != hex.EncodeToString(b[:]) {
		t.Fatal("unstable")
	}
}

func TestContentHashChangesWhenFixturePriceChanges(t *testing.T) {
	file, err := os.Open("../../../testdata/fixtures/listings.json")
	if err != nil {
		t.Fatal(err)
	}
	listings, err := parser.ParseJSON(file, "fixture")
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) == 0 || listings[0].Price == nil {
		t.Fatal("fixture has no price")
	}
	original := sha256.Sum256([]byte(normalize.Hashable(listings[0])))
	changed := listings[0]
	price := *changed.Price + 1
	changed.Price = &price
	updated := sha256.Sum256([]byte(normalize.Hashable(changed)))
	if hex.EncodeToString(original[:]) == hex.EncodeToString(updated[:]) {
		t.Fatal("price change did not change content hash")
	}
}
