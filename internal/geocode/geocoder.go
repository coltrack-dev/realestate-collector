package geocode

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/example/realestate-collector/internal/model"
)

type Precision string

const (
	Exact        Precision = "EXACT"
	Address      Precision = "ADDRESS"
	Neighborhood Precision = "NEIGHBORHOOD"
	City         Precision = "CITY"
	Department   Precision = "DEPARTMENT"
	Country      Precision = "COUNTRY"
	Unknown      Precision = "UNKNOWN"
)

type Result struct {
	Latitude   float64
	Longitude  float64
	Precision  Precision
	Provider   string
	ResolvedAt time.Time
}

type Geocoder interface {
	Resolve(context.Context, model.Listing) (Result, error)
}

// LocalDirectory resolves only to curated administrative centers. It never
// claims that a city or country center is the property's exact location.
type LocalDirectory struct{}

func (LocalDirectory) Resolve(ctx context.Context, listing model.Listing) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	code := strings.ToUpper(strings.TrimSpace(listing.CountryCode))
	if code == "" {
		return Result{}, fmt.Errorf("country code is required for local geocoding")
	}
	if center, ok := cityCenter(code, listing.City); ok {
		return result(center[0], center[1], City), nil
	}
	if center, ok := departmentCenter(code, listing.Department); ok {
		return result(center[0], center[1], Department), nil
	}
	if center, ok := countryCenter(code); ok {
		return result(center[0], center[1], Country), nil
	}
	return Result{Precision: Unknown, Provider: "local-directory", ResolvedAt: time.Now().UTC()}, nil
}

func result(latitude, longitude float64, precision Precision) Result {
	return Result{Latitude: latitude, Longitude: longitude, Precision: precision, Provider: "local-directory", ResolvedAt: time.Now().UTC()}
}

func countryCenter(code string) ([2]float64, bool) {
	switch code {
	case "CO":
		return [2]float64{4.5709, -74.2973}, true
	case "UY":
		return [2]float64{-32.5228, -56.1645}, true
	default:
		return [2]float64{}, false
	}
}

func cityCenter(code, city string) ([2]float64, bool) {
	key := strings.ToLower(strings.TrimSpace(city))
	switch code {
	case "CO":
		switch key {
		case "bogotá d.c.", "bogota", "bogotá":
			return [2]float64{4.7110, -74.0721}, true
		case "medellín", "medellin":
			return [2]float64{6.2442, -75.5812}, true
		case "rionegro":
			return [2]float64{6.1550, -75.3740}, true
		}
	case "UY":
		switch key {
		case "montevideo":
			return [2]float64{-34.9011, -56.1645}, true
		case "maldonado":
			return [2]float64{-34.9000, -54.9500}, true
		case "punta del este":
			return [2]float64{-34.9500, -54.9333}, true
		}
	}
	return [2]float64{}, false
}

func departmentCenter(code, department string) ([2]float64, bool) {
	key := strings.ToLower(strings.TrimSpace(department))
	switch code {
	case "CO":
		if key == "antioquia" {
			return [2]float64{6.2518, -75.5636}, true
		}
		if key == "cundinamarca" {
			return [2]float64{4.7318, -74.2647}, true
		}
	case "UY":
		if key == "montevideo" {
			return [2]float64{-34.9011, -56.1645}, true
		}
		if key == "maldonado" {
			return [2]float64{-34.9000, -54.9500}, true
		}
	}
	return [2]float64{}, false
}
