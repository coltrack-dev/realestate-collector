package geocode

import (
	"context"
	"testing"

	"github.com/example/realestate-collector/internal/model"
)

func TestLocalDirectoryKeepsCountriesSeparate(t *testing.T) {
	geocoder := LocalDirectory{}
	colombia, err := geocoder.Resolve(context.Background(), model.Listing{CountryCode: "CO", City: "Montevideo"})
	if err != nil || colombia.Precision != Country || colombia.Latitude != 4.5709 {
		t.Fatalf("unexpected Colombia result: %#v, %v", colombia, err)
	}
	uruguay, err := geocoder.Resolve(context.Background(), model.Listing{CountryCode: "UY", City: "Montevideo"})
	if err != nil || uruguay.Precision != City || uruguay.Longitude != -56.1645 {
		t.Fatalf("unexpected Uruguay result: %#v, %v", uruguay, err)
	}
}

func TestLocalDirectoryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (LocalDirectory{}).Resolve(ctx, model.Listing{CountryCode: "UY", City: "Montevideo"}); err == nil {
		t.Fatal("expected context cancellation")
	}
}
