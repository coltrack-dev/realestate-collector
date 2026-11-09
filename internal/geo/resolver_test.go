package geo

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestFixtureResolvesTargetColombianCities(t *testing.T) {
	file, err := os.Open("../../testdata/fixtures/geo/municipalities.geojson")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	resolver, err := NewFromReader(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		point Point
		city  string
	}{
		{name: "Bogota", point: Point{Longitude: -74.07, Latitude: 4.71}, city: "Bogotá D.C."},
		{name: "Cali", point: Point{Longitude: -76.53, Latitude: 3.44}, city: "Cali"},
		{name: "Medellin", point: Point{Longitude: -75.58, Latitude: 6.24}, city: "Medellín"},
		{name: "Rionegro", point: Point{Longitude: -75.37, Latitude: 6.15}, city: "Rionegro"},
		{name: "Cartagena", point: Point{Longitude: -75.50, Latitude: 10.40}, city: "Cartagena"},
		{name: "Barranquilla", point: Point{Longitude: -74.80, Latitude: 10.98}, city: "Barranquilla"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := resolver.Resolve(context.Background(), test.point)
			if err != nil || result.Municipality != test.city {
				t.Fatalf("unexpected result: %+v, %v", result, err)
			}
		})
	}
}

func TestPolygonResolverResolvesMunicipalityAndCountry(t *testing.T) {
	resolver, err := NewFromReader(strings.NewReader(`{
  "type":"FeatureCollection","features":[
    {"type":"Feature","properties":{"country_code":"CO","department":"Valle del Cauca","municipality":"Cali"},"geometry":{"type":"Polygon","coordinates":[[[-76,3],[-76,4],[-75,4],[-75,3],[-76,3]]]}}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := resolver.Resolve(context.Background(), Point{Longitude: -75.5, Latitude: 3.5})
	if err != nil || result.CountryCode != "CO" || result.Municipality != "Cali" || result.Resolution != AdministrativePolygon {
		t.Fatalf("unexpected resolution: %+v, %v", result, err)
	}
}

func TestPolygonResolverReturnsUnknownOutsidePolygons(t *testing.T) {
	resolver, err := NewFromReader(strings.NewReader(`{"type":"FeatureCollection","features":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := resolver.Resolve(context.Background(), Point{Longitude: -74, Latitude: 4})
	if err != nil || result.Resolution != Unknown || result.CountryCode != "" {
		t.Fatalf("unexpected unknown result: %+v, %v", result, err)
	}
}

func TestEmptyResolverDoesNotInventGeography(t *testing.T) {
	result, err := (EmptyResolver{}).Resolve(context.Background(), Point{Longitude: -74, Latitude: 4})
	if err != nil || result.Resolution != Unknown {
		t.Fatalf("unexpected empty resolver result: %+v, %v", result, err)
	}
}
