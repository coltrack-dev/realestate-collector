package geo

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
)

func TestFullColombiaSnapshotResolvesMunicipalities(t *testing.T) {
	resolver, err := NewFromFile("../../data/geo/colombia_municipalities.geojson")
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := resolver.(BoundaryMetadata)
	if !ok || metadata.BoundaryCount() != 1121 {
		t.Fatalf("unexpected boundary metadata: %v", metadata)
	}
	for _, test := range []struct {
		name                                        string
		point                                       Point
		country, department, municipality, divipola string
	}{
		{"Baranoa", Point{Longitude: -74.92, Latitude: 10.79}, "CO", "ATLÁNTICO", "BARANOA", "08078"},
		{"Cartagena", Point{Longitude: -75.50, Latitude: 10.40}, "CO", "BOLÍVAR", "CARTAGENA DE INDIAS", "13001"},
		{"Bogota", Point{Longitude: -74.07, Latitude: 4.71}, "CO", "BOGOTÁ, D.C.", "BOGOTÁ, D.C.", "11001"},
		{"Barranquilla", Point{Longitude: -74.80, Latitude: 10.98}, "CO", "ATLÁNTICO", "BARRANQUILLA", "08001"},
		{"Medellin", Point{Longitude: -75.58, Latitude: 6.24}, "CO", "ANTIOQUIA", "MEDELLÍN", "05001"},
		{"Rionegro", Point{Longitude: -75.37, Latitude: 6.15}, "CO", "ANTIOQUIA", "RIONEGRO", "05615"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := resolver.Resolve(context.Background(), test.point)
			if err != nil || result.CountryCode != test.country || result.Department != test.department || result.Municipality != test.municipality || result.DIVIPOLA != test.divipola {
				t.Fatalf("unexpected result: %+v, %v", result, err)
			}
		})
	}
	viewport, err := resolver.(ViewportResolver).ResolveViewport(context.Background(), BBox{MinLongitude: -75.6, MaxLongitude: -75.3, MinLatitude: 6.05, MaxLatitude: 6.25})
	if err != nil || len(viewport.Municipalities) < 2 {
		t.Fatalf("expected multi-municipality viewport: %+v, %v", viewport, err)
	}
	for _, municipality := range viewport.Municipalities {
		if municipality.OverlapAreaM2 <= 0 || math.IsNaN(municipality.OverlapRatio) {
			t.Fatalf("invalid overlap metrics: %+v", municipality)
		}
	}
	unknown, err := resolver.Resolve(context.Background(), Point{Longitude: -40, Latitude: 0})
	if err != nil || unknown.Resolution != Unknown {
		t.Fatalf("expected unknown outside Colombia: %+v, %v", unknown, err)
	}
}

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

func TestResolveViewportReturnsIntersectingMunicipalitiesAndMetrics(t *testing.T) {
	resolver, err := NewFromReader(strings.NewReader(`{"type":"FeatureCollection","features":[
{"type":"Feature","properties":{"country_code":"CO","department":"Antioquia","municipality":"Rionegro","divipola":"05615"},"geometry":{"type":"Polygon","coordinates":[[[-76,6],[-75,6],[-75,7],[-76,7],[-76,6]]]}},
{"type":"Feature","properties":{"country_code":"CO","department":"Antioquia","municipality":"Marinilla","divipola":"05440"},"geometry":{"type":"MultiPolygon","coordinates":[[[[-75,6],[-74.5,6],[-74.5,6.5],[-75,6.5],[-75,6]]],[[[-74,6],[-73.5,6],[-73.5,6.5],[-74,6.5],[-74,6]]]]}}
]}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := resolver.(ViewportResolver).ResolveViewport(context.Background(), BBox{MinLongitude: -75.2, MaxLongitude: -74.8, MinLatitude: 6.1, MaxLatitude: 6.4})
	if err != nil || len(result.Municipalities) != 2 {
		t.Fatalf("unexpected municipalities: %+v, %v", result, err)
	}
	for _, municipality := range result.Municipalities {
		if municipality.OverlapAreaM2 <= 0 || municipality.OverlapRatio <= 0 || municipality.DIVIPOLA == "" {
			t.Fatalf("missing metric or divipola: %+v", municipality)
		}
	}
}

func TestResolveViewportExcludesPolygonHole(t *testing.T) {
	resolver, err := NewFromReader(strings.NewReader(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"country_code":"CO","municipality":"Test"},"geometry":{"type":"Polygon","coordinates":[[[-1,-1],[-1,1],[1,1],[1,-1],[-1,-1]],[[-0.5,-0.5],[-0.5,0.5],[0.5,0.5],[0.5,-0.5],[-0.5,-0.5]]]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	viewport := resolver.(ViewportResolver)
	insideHole, err := viewport.ResolveViewport(context.Background(), BBox{MinLongitude: -0.2, MaxLongitude: 0.2, MinLatitude: -0.2, MaxLatitude: 0.2})
	if err != nil || len(insideHole.Municipalities) != 0 {
		t.Fatalf("hole was treated as municipality: %+v, %v", insideHole, err)
	}
	intersects, err := viewport.ResolveViewport(context.Background(), BBox{MinLongitude: 0.4, MaxLongitude: 0.8, MinLatitude: 0.4, MaxLatitude: 0.8})
	if err != nil || len(intersects.Municipalities) != 1 || intersects.Municipalities[0].OverlapAreaM2 <= 0 {
		t.Fatalf("outer area was not found: %+v, %v", intersects, err)
	}
}
