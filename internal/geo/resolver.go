package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type Resolution string

const (
	AdministrativePolygon Resolution = "ADMINISTRATIVE_POLYGON"
	Unknown               Resolution = "UNKNOWN"
)

type Point struct {
	Longitude float64
	Latitude  float64
}

type Result struct {
	CountryCode      string     `json:"country_code"`
	Department       string     `json:"department,omitempty"`
	Municipality     string     `json:"municipality,omitempty"`
	Resolution       Resolution `json:"resolution"`
	AvailableSources []string   `json:"available_sources,omitempty"`
}

type Resolver interface {
	Resolve(context.Context, Point) (Result, error)
}

type polygonResolver struct {
	boundaries []boundary
}

type boundary struct {
	countryCode  string
	department   string
	municipality string
	polygons     [][]ring
}

type geoJSON struct {
	Features []struct {
		Properties struct {
			CountryCode  string `json:"country_code"`
			Department   string `json:"department"`
			Municipality string `json:"municipality"`
		} `json:"properties"`
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

type ring = []Point

func NewFromFile(path string) (Resolver, error) {
	if strings.TrimSpace(path) == "" {
		return EmptyResolver{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open geographic boundaries: %w", err)
	}
	defer file.Close()
	return NewFromReader(file)
}

func NewFromReader(reader io.Reader) (Resolver, error) {
	var document geoJSON
	if err := json.NewDecoder(reader).Decode(&document); err != nil {
		return nil, fmt.Errorf("decode geographic boundaries: %w", err)
	}
	resolver := &polygonResolver{}
	for index, feature := range document.Features {
		geometry, err := parseGeometry(feature.Geometry.Type, feature.Geometry.Coordinates)
		if err != nil {
			return nil, fmt.Errorf("feature %d: %w", index, err)
		}
		if feature.Properties.CountryCode == "" || feature.Properties.Municipality == "" || len(geometry) == 0 {
			continue
		}
		resolver.boundaries = append(resolver.boundaries, boundary{
			countryCode: strings.ToUpper(strings.TrimSpace(feature.Properties.CountryCode)),
			department:  feature.Properties.Department, municipality: feature.Properties.Municipality,
			polygons: geometry,
		})
	}
	return resolver, nil
}

func (r polygonResolver) Resolve(ctx context.Context, point Point) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	for _, boundary := range r.boundaries {
		for _, polygon := range boundary.polygons {
			if pointInPolygon(point, polygon) {
				return Result{CountryCode: boundary.countryCode, Department: boundary.department, Municipality: boundary.municipality, Resolution: AdministrativePolygon}, nil
			}
		}
	}
	return Result{Resolution: Unknown}, nil
}

type EmptyResolver struct{}

func (EmptyResolver) Resolve(ctx context.Context, _ Point) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return Result{Resolution: Unknown}, nil
}

func parseGeometry(kind string, raw json.RawMessage) ([][]ring, error) {
	switch kind {
	case "Polygon":
		var coordinates [][][]float64
		if err := json.Unmarshal(raw, &coordinates); err != nil {
			return nil, err
		}
		return [][]ring{convertPolygon(coordinates)}, nil
	case "MultiPolygon":
		var coordinates [][][][]float64
		if err := json.Unmarshal(raw, &coordinates); err != nil {
			return nil, err
		}
		result := make([][]ring, 0, len(coordinates))
		for _, polygon := range coordinates {
			result = append(result, convertPolygon(polygon))
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported geometry %q", kind)
	}
}

func convertPolygon(coordinates [][][]float64) []ring {
	result := make([]ring, 0, len(coordinates))
	for _, rawRing := range coordinates {
		ring := make(ring, 0, len(rawRing))
		for _, pair := range rawRing {
			if len(pair) >= 2 {
				ring = append(ring, Point{Longitude: pair[0], Latitude: pair[1]})
			}
		}
		if len(ring) >= 3 {
			result = append(result, ring)
		}
	}
	return result
}

func pointInPolygon(point Point, polygon []ring) bool {
	if len(polygon) == 0 || !pointInRing(point, polygon[0]) {
		return false
	}
	for _, hole := range polygon[1:] {
		if pointInRing(point, hole) {
			return false
		}
	}
	return true
}

func pointInRing(point Point, ring ring) bool {
	inside := false
	for index, current := range ring {
		previous := ring[(index+len(ring)-1)%len(ring)]
		intersects := (current.Latitude > point.Latitude) != (previous.Latitude > point.Latitude) && point.Longitude < (previous.Longitude-current.Longitude)*(point.Latitude-current.Latitude)/(previous.Latitude-current.Latitude)+current.Longitude
		if intersects {
			inside = !inside
		}
	}
	return inside
}

var _ Resolver = polygonResolver{}
var _ Resolver = EmptyResolver{}
