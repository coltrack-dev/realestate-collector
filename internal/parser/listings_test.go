package parser

import (
	"strings"
	"testing"
)

func TestParseListingsJSONNormalizesNullableFields(t *testing.T) {
	input := `[{"external_id":"json-1","url":"https://example.test/1?utm_campaign=x","operation":"Venta","property_type":"Apartamento","price":"$ 850.000.000","currency":"COP","area":"78,5 m2","bedrooms":3,"bathrooms":null,"city":"Bogota"}]`
	listings, err := ParseListingsJSON(strings.NewReader(input), "json-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 1 || listings[0].Source != "json-fixture" {
		t.Fatalf("unexpected listings: %#v", listings)
	}
	if listings[0].Price == nil || *listings[0].Price != 850000000 {
		t.Fatalf("unexpected price: %#v", listings[0].Price)
	}
	if listings[0].AreaM2 == nil || *listings[0].AreaM2 != 78.5 {
		t.Fatalf("unexpected area: %#v", listings[0].AreaM2)
	}
	if listings[0].Bathrooms != nil {
		t.Fatalf("null bathroom became a value: %#v", listings[0].Bathrooms)
	}
	if listings[0].Operation != "SALE" || listings[0].PropertyType != "APARTMENT" || listings[0].City != "Bogotá D.C." {
		t.Fatalf("normalization failed: %#v", listings[0])
	}
	if listings[0].URL != "https://example.test/1" {
		t.Fatalf("URL was not canonicalized: %q", listings[0].URL)
	}
}

func TestParseListingsCSVSupportsCurrenciesAndImages(t *testing.T) {
	input := "source,external_id,url,operation,property_type,price,currency,area_m2,images\n" +
		"csv-fixture,1,https://example.test/1,Rent,Apartment,2500000,COP,62.5,\"https://example.test/a.jpg;https://example.test/b.jpg\"\n"
	listings, err := ParseListingsCSV(strings.NewReader(input), "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 1 || listings[0].Currency != "COP" {
		t.Fatalf("unexpected CSV listing: %#v", listings)
	}
	if listings[0].Operation != "RENT" || len(listings[0].Images) != 2 {
		t.Fatalf("CSV values were not parsed: %#v", listings[0])
	}
}

func TestParseListingsJSONRejectsInvalidMoney(t *testing.T) {
	input := `[{"external_id":"bad","url":"https://example.test/bad","operation":"sale","property_type":"apartment","price":"not-a-number"}]`
	if _, err := ParseListingsJSON(strings.NewReader(input), "fixture"); err == nil {
		t.Fatal("expected invalid money error")
	}
}
