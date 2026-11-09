package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

type Resolution string

const (
	AdministrativePolygon Resolution = "ADMINISTRATIVE_POLYGON"
	Unknown               Resolution = "UNKNOWN"
)

type Point struct {
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
}

type BBox struct {
	MinLongitude float64 `json:"min_longitude"`
	MaxLongitude float64 `json:"max_longitude"`
	MinLatitude  float64 `json:"min_latitude"`
	MaxLatitude  float64 `json:"max_latitude"`
}

func (b BBox) Valid() bool {
	return !math.IsNaN(b.MinLongitude) && !math.IsNaN(b.MaxLongitude) && !math.IsNaN(b.MinLatitude) && !math.IsNaN(b.MaxLatitude) && !math.IsInf(b.MinLongitude, 0) && !math.IsInf(b.MaxLongitude, 0) && !math.IsInf(b.MinLatitude, 0) && !math.IsInf(b.MaxLatitude, 0) && b.MinLongitude >= -180 && b.MaxLongitude <= 180 && b.MinLatitude >= -90 && b.MaxLatitude <= 90 && b.MinLongitude <= b.MaxLongitude && b.MinLatitude <= b.MaxLatitude
}

type Result struct {
	CountryCode      string     `json:"country_code"`
	Department       string     `json:"department,omitempty"`
	DepartmentID     string     `json:"department_id,omitempty"`
	Municipality     string     `json:"municipality,omitempty"`
	MunicipalityID   string     `json:"municipality_id,omitempty"`
	Locality         string     `json:"locality,omitempty"`
	LocalityID       string     `json:"locality_id,omitempty"`
	DIVIPOLA         string     `json:"divipola,omitempty"`
	Resolution       Resolution `json:"resolution"`
	AvailableSources []string   `json:"available_sources,omitempty"`
}

type ViewportMunicipality struct {
	CountryCode      string   `json:"country_code"`
	Department       string   `json:"department,omitempty"`
	Municipality     string   `json:"municipality,omitempty"`
	DIVIPOLA         string   `json:"divipola,omitempty"`
	OverlapAreaM2    float64  `json:"overlap_area_m2"`
	OverlapRatio     float64  `json:"overlap_ratio"`
	AvailableSources []string `json:"available_sources,omitempty"`
}

type ViewportArea struct {
	Level            string   `json:"level"`
	ID               string   `json:"id,omitempty"`
	CountryCode      string   `json:"country_code"`
	Department       string   `json:"department,omitempty"`
	DepartmentID     string   `json:"department_id,omitempty"`
	Municipality     string   `json:"municipality,omitempty"`
	MunicipalityID   string   `json:"municipality_id,omitempty"`
	Locality         string   `json:"locality,omitempty"`
	LocalityID       string   `json:"locality_id,omitempty"`
	OverlapAreaM2    float64  `json:"overlap_area_m2"`
	OverlapRatio     float64  `json:"overlap_ratio"`
	AvailableSources []string `json:"available_sources,omitempty"`
}

type ViewportResult struct {
	Municipalities []ViewportMunicipality `json:"municipalities"`
	Areas          []ViewportArea         `json:"areas"`
}

type Resolver interface {
	Resolve(context.Context, Point) (Result, error)
}

type ViewportResolver interface {
	ResolveViewport(context.Context, BBox) (ViewportResult, error)
}

type BoundaryMetadata interface {
	BoundaryCount() int
	BoundarySource() string
	BoundaryVersion() string
	BoundaryCounts() map[string]int
}

type polygonResolver struct {
	boundaries []boundary
	source     string
	version    string
}

type boundary struct {
	countryCode    string
	level          string
	id             string
	department     string
	departmentID   string
	municipality   string
	municipalityID string
	locality       string
	localityID     string
	divipola       string
	polygons       [][]ring
	minLongitude   float64
	maxLongitude   float64
	minLatitude    float64
	maxLatitude    float64
}

type geoJSON struct {
	Features []struct {
		Properties struct {
			CountryCode    string `json:"country_code"`
			Level          string `json:"level"`
			ID             string `json:"id"`
			Department     string `json:"department"`
			DepartmentID   string `json:"department_id"`
			Municipality   string `json:"municipality"`
			MunicipalityID string `json:"municipality_id"`
			Locality       string `json:"locality"`
			LocalityID     string `json:"locality_id"`
			DIVIPOLA       string `json:"divipola"`
		} `json:"properties"`
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
	Name string `json:"name"`
}

type ring = []Point

func NewFromFile(path string) (Resolver, error) {
	return NewFromFiles([]string{path})
}

func NewFromFiles(paths []string) (Resolver, error) {
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			filtered = append(filtered, path)
		}
	}
	if len(filtered) == 0 {
		return EmptyResolver{}, nil
	}
	combined := &polygonResolver{source: strings.Join(filtered, ",")}
	for _, path := range filtered {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open geographic boundaries: %w", err)
		}
		resolver, parseErr := newFromReader(file)
		closeErr := file.Close()
		if parseErr != nil {
			return nil, parseErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		combined.boundaries = append(combined.boundaries, resolver.boundaries...)
		if combined.version == "" {
			combined.version = resolver.version
		} else if resolver.version != "" {
			combined.version += "," + resolver.version
		}
	}
	return combined, nil
}

func newSingleFileResolver(path string) (Resolver, error) {
	if strings.TrimSpace(path) == "" {
		return EmptyResolver{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open geographic boundaries: %w", err)
	}
	defer file.Close()
	resolver, err := newFromReader(file)
	if err != nil {
		return nil, err
	}
	resolver.source = path
	return resolver, nil
}

func NewFromReader(reader io.Reader) (Resolver, error) {
	return newFromReader(reader)
}

func newFromReader(reader io.Reader) (*polygonResolver, error) {
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
		level := strings.ToUpper(strings.TrimSpace(feature.Properties.Level))
		if level == "" {
			level = "MUNICIPALITY"
		}
		if feature.Properties.CountryCode == "" || len(geometry) == 0 {
			continue
		}
		if level == "DEPARTMENT" && feature.Properties.Department == "" {
			continue
		}
		if level == "MUNICIPALITY" && feature.Properties.Municipality == "" {
			continue
		}
		if level == "LOCALITY" && feature.Properties.Locality == "" {
			continue
		}
		id := feature.Properties.ID
		if id == "" {
			id = feature.Properties.DIVIPOLA
			if id == "" {
				id = feature.Properties.DepartmentID
			}
			if id == "" {
				id = feature.Properties.MunicipalityID
			}
			if id == "" {
				id = feature.Properties.LocalityID
			}
		}
		if id == "" {
			switch level {
			case "DEPARTMENT":
				id = feature.Properties.Department
			case "LOCALITY":
				id = feature.Properties.Locality
			default:
				id = feature.Properties.Municipality
			}
		}
		minLongitude, maxLongitude, minLatitude, maxLatitude := geometryBounds(geometry)
		resolver.boundaries = append(resolver.boundaries, boundary{
			countryCode: strings.ToUpper(strings.TrimSpace(feature.Properties.CountryCode)),
			level:       level, id: id, department: feature.Properties.Department, departmentID: feature.Properties.DepartmentID, municipality: feature.Properties.Municipality, municipalityID: feature.Properties.MunicipalityID, locality: feature.Properties.Locality, localityID: feature.Properties.LocalityID, divipola: feature.Properties.DIVIPOLA,
			polygons:     geometry,
			minLongitude: minLongitude, maxLongitude: maxLongitude, minLatitude: minLatitude, maxLatitude: maxLatitude,
		})
	}
	resolver.version = document.Name
	return resolver, nil
}

func (r polygonResolver) BoundaryCount() int      { return len(r.boundaries) }
func (r polygonResolver) BoundarySource() string  { return r.source }
func (r polygonResolver) BoundaryVersion() string { return r.version }
func (r polygonResolver) BoundaryCounts() map[string]int {
	counts := make(map[string]int)
	for _, boundary := range r.boundaries {
		counts[boundary.countryCode+":"+boundary.level]++
	}
	return counts
}

func geometryBounds(polygons [][]ring) (minLongitude, maxLongitude, minLatitude, maxLatitude float64) {
	minLongitude, minLatitude = math.Inf(1), math.Inf(1)
	maxLongitude, maxLatitude = math.Inf(-1), math.Inf(-1)
	for _, polygon := range polygons {
		for _, currentRing := range polygon {
			for _, point := range currentRing {
				if point.Longitude < minLongitude {
					minLongitude = point.Longitude
				}
				if point.Longitude > maxLongitude {
					maxLongitude = point.Longitude
				}
				if point.Latitude < minLatitude {
					minLatitude = point.Latitude
				}
				if point.Latitude > maxLatitude {
					maxLatitude = point.Latitude
				}
			}
		}
	}
	return
}

func (r polygonResolver) Resolve(ctx context.Context, point Point) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	matches := make([]boundary, 0)
	for _, boundary := range r.boundaries {
		if point.Longitude < boundary.minLongitude || point.Longitude > boundary.maxLongitude || point.Latitude < boundary.minLatitude || point.Latitude > boundary.maxLatitude {
			continue
		}
		for _, polygon := range boundary.polygons {
			if pointInPolygon(point, polygon) {
				matches = append(matches, boundary)
				break
			}
		}
	}
	if len(matches) == 0 {
		return Result{Resolution: Unknown}, nil
	}
	best := matches[0]
	for _, candidate := range matches[1:] {
		if levelRank(candidate.level) > levelRank(best.level) {
			best = candidate
		}
	}
	result := Result{CountryCode: best.countryCode, Department: best.department, DepartmentID: best.departmentID, Municipality: best.municipality, MunicipalityID: best.municipalityID, Locality: best.locality, LocalityID: best.localityID, DIVIPOLA: best.divipola, Resolution: AdministrativePolygon}
	for _, candidate := range matches {
		if result.Department == "" {
			result.Department = candidate.department
		}
		if result.DepartmentID == "" {
			result.DepartmentID = candidate.departmentID
		}
		if result.Municipality == "" {
			result.Municipality = candidate.municipality
		}
		if result.MunicipalityID == "" {
			result.MunicipalityID = candidate.municipalityID
		}
		if result.Locality == "" {
			result.Locality = candidate.locality
		}
		if result.LocalityID == "" {
			result.LocalityID = candidate.localityID
		}
	}
	return result, nil
}

func levelRank(level string) int {
	switch strings.ToUpper(level) {
	case "LOCALITY":
		return 3
	case "MUNICIPALITY":
		return 2
	case "DEPARTMENT":
		return 1
	default:
		return 2
	}
}

func (r polygonResolver) ResolveViewport(ctx context.Context, box BBox) (ViewportResult, error) {
	if err := ctx.Err(); err != nil {
		return ViewportResult{}, err
	}
	if !box.Valid() {
		return ViewportResult{}, fmt.Errorf("viewport bbox is invalid")
	}
	latitude := (box.MinLatitude + box.MaxLatitude) / 2
	result := ViewportResult{Municipalities: make([]ViewportMunicipality, 0), Areas: make([]ViewportArea, 0)}
	indices := make(map[string]int)
	totals := make([]float64, 0)
	for _, boundary := range r.boundaries {
		if box.MaxLongitude < boundary.minLongitude || box.MinLongitude > boundary.maxLongitude || box.MaxLatitude < boundary.minLatitude || box.MinLatitude > boundary.maxLatitude {
			continue
		}
		key := boundary.countryCode + "\x00" + boundary.level + "\x00" + boundary.id
		total, overlap := 0.0, 0.0
		for _, polygon := range boundary.polygons {
			total += polygonAreaM2(polygon, latitude)
			overlap += polygonIntersectionAreaM2(polygon, box, latitude)
		}
		if overlap <= 0 {
			continue
		}
		if index, exists := indices[key]; exists {
			result.Areas[index].OverlapAreaM2 += overlap
			totals[index] += total
			continue
		}
		indices[key] = len(result.Areas)
		totals = append(totals, total)
		result.Areas = append(result.Areas, ViewportArea{Level: boundary.level, ID: boundary.id, CountryCode: boundary.countryCode, Department: boundary.department, DepartmentID: boundary.departmentID, Municipality: boundary.municipality, MunicipalityID: boundary.municipalityID, Locality: boundary.locality, LocalityID: boundary.localityID, OverlapAreaM2: overlap})
	}
	for index := range result.Areas {
		if totals[index] > 0 {
			result.Areas[index].OverlapRatio = result.Areas[index].OverlapAreaM2 / totals[index]
		}
		if result.Areas[index].OverlapRatio > 1 {
			result.Areas[index].OverlapRatio = 1
		}
		if result.Areas[index].Level == "MUNICIPALITY" {
			area := result.Areas[index]
			result.Municipalities = append(result.Municipalities, ViewportMunicipality{CountryCode: area.CountryCode, Department: area.Department, Municipality: area.Municipality, DIVIPOLA: area.ID, OverlapAreaM2: area.OverlapAreaM2, OverlapRatio: area.OverlapRatio})
		}
	}
	return result, nil
}

type EmptyResolver struct{}

func (EmptyResolver) Resolve(ctx context.Context, _ Point) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return Result{Resolution: Unknown}, nil
}

func (EmptyResolver) ResolveViewport(ctx context.Context, _ BBox) (ViewportResult, error) {
	if err := ctx.Err(); err != nil {
		return ViewportResult{}, err
	}
	return ViewportResult{Municipalities: []ViewportMunicipality{}}, nil
}

const earthRadiusM = 6371008.8

func project(point Point, latitude float64) Point {
	const radians = math.Pi / 180
	return Point{Longitude: point.Longitude * radians * earthRadiusM * math.Cos(latitude*radians), Latitude: point.Latitude * radians * earthRadiusM}
}

func projectRing(source ring, latitude float64) ring {
	result := make(ring, len(source))
	for i, point := range source {
		result[i] = project(point, latitude)
	}
	return result
}

func polygonAreaM2(polygon []ring, latitude float64) float64 {
	if len(polygon) == 0 {
		return 0
	}
	area := math.Abs(ringArea(projectRing(polygon[0], latitude)))
	for _, hole := range polygon[1:] {
		area -= math.Abs(ringArea(projectRing(hole, latitude)))
	}
	if area < 0 {
		return 0
	}
	return area
}

func polygonIntersectionAreaM2(polygon []ring, box BBox, latitude float64) float64 {
	if len(polygon) == 0 {
		return 0
	}
	viewport := ring{{box.MinLongitude, box.MinLatitude}, {box.MaxLongitude, box.MinLatitude}, {box.MaxLongitude, box.MaxLatitude}, {box.MinLongitude, box.MaxLatitude}}
	area := math.Abs(ringArea(clipRing(projectRing(polygon[0], latitude), projectRing(viewport, latitude))))
	for _, hole := range polygon[1:] {
		area -= math.Abs(ringArea(clipRing(projectRing(hole, latitude), projectRing(viewport, latitude))))
	}
	if area < 0 {
		return 0
	}
	return area
}

func ringArea(points ring) float64 {
	if len(points) < 3 {
		return 0
	}
	area := 0.0
	for i, point := range points {
		next := points[(i+1)%len(points)]
		area += point.Longitude*next.Latitude - next.Longitude*point.Latitude
	}
	return area / 2
}

func clipRing(subject, clip ring) ring {
	output := append(ring(nil), subject...)
	for i, start := range clip {
		if len(output) == 0 {
			break
		}
		end := clip[(i+1)%len(clip)]
		input := output
		output = nil
		for j, current := range input {
			previous := input[(j+len(input)-1)%len(input)]
			ci := insideEdge(current, start, end)
			pi := insideEdge(previous, start, end)
			if ci != pi {
				output = append(output, edgeIntersection(previous, current, start, end))
			}
			if ci {
				output = append(output, current)
			}
		}
	}
	return output
}

func insideEdge(point, start, end Point) bool {
	return (end.Longitude-start.Longitude)*(point.Latitude-start.Latitude)-(end.Latitude-start.Latitude)*(point.Longitude-start.Longitude) >= -1e-9
}

func edgeIntersection(a, b, start, end Point) Point {
	dx, dy := b.Longitude-a.Longitude, b.Latitude-a.Latitude
	ex, ey := end.Longitude-start.Longitude, end.Latitude-start.Latitude
	denominator := ex*dy - ey*dx
	if math.Abs(denominator) < 1e-12 {
		return a
	}
	t := (ex*(start.Latitude-a.Latitude) - ey*(start.Longitude-a.Longitude)) / denominator
	return Point{Longitude: a.Longitude + t*dx, Latitude: a.Latitude + t*dy}
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
var _ ViewportResolver = polygonResolver{}
var _ ViewportResolver = EmptyResolver{}
