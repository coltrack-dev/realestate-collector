package parser

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/normalize"
)

// ParseListingsJSON parses either a JSON array of listings or an object with a
// listings/data array. Source is used when an input row does not contain it.
func ParseListingsJSON(r io.Reader, source string) ([]model.Listing, error) {
	var value any
	decoder := json.NewDecoder(r)
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode listings JSON: %w", err)
	}

	rows, err := jsonRows(value)
	if err != nil {
		return nil, err
	}
	return parseListingRows(rows, source)
}

// ParseListingsCSV parses a CSV file whose headers use the Listing field names
// or their snake_case equivalents. Images may be a JSON array or a comma/
// semicolon-separated list.
func ParseListingsCSV(r io.Reader, source string) ([]model.Listing, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	keys := make([]string, len(headers))
	for i, header := range headers {
		keys[i] = fieldKey(header)
	}

	var rows []map[string]any
	for rowNumber := 2; ; rowNumber++ {
		values, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read CSV row %d: %w", rowNumber, readErr)
		}
		row := make(map[string]any, len(keys))
		for i, key := range keys {
			if i < len(values) {
				row[key] = values[i]
			}
		}
		rows = append(rows, row)
	}
	return parseListingRows(rows, source)
}

func jsonRows(value any) ([]map[string]any, error) {
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"listings", "data", "items"} {
			if nested, exists := object[key]; exists {
				value = nested
				break
			}
		}
	}
	array, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("listings JSON must be an array or contain a listings/data/items array")
	}
	rows := make([]map[string]any, 0, len(array))
	for index, item := range array {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("listing JSON row %d is not an object", index+1)
		}
		normalized := make(map[string]any, len(object))
		for key, itemValue := range object {
			normalized[fieldKey(key)] = itemValue
		}
		rows = append(rows, normalized)
	}
	return rows, nil
}

func parseListingRows(rows []map[string]any, source string) ([]model.Listing, error) {
	listings := make([]model.Listing, 0, len(rows))
	for index, row := range rows {
		listing, err := listingFromRow(row, source)
		if err != nil {
			return nil, fmt.Errorf("parse listing row %d: %w", index+1, err)
		}
		listings = append(listings, normalize.CleanListing(listing))
	}
	return listings, nil
}

func listingFromRow(row map[string]any, defaultSource string) (model.Listing, error) {
	value := func(names ...string) any {
		for _, name := range names {
			if found, ok := row[fieldKey(name)]; ok {
				return found
			}
		}
		return nil
	}
	text := func(names ...string) string { return stringValue(value(names...)) }

	price, err := moneyValue(value("price"))
	if err != nil {
		return model.Listing{}, fmt.Errorf("price: %w", err)
	}
	area, err := decimalValue(value("area_m2", "area", "surface"))
	if err != nil {
		return model.Listing{}, fmt.Errorf("area_m2: %w", err)
	}
	bedrooms, err := intPointer(value("bedrooms", "rooms"))
	if err != nil {
		return model.Listing{}, fmt.Errorf("bedrooms: %w", err)
	}
	bathrooms, err := intPointer(value("bathrooms"))
	if err != nil {
		return model.Listing{}, fmt.Errorf("bathrooms: %w", err)
	}
	parking, err := intPointer(value("parking_spots", "parking"))
	if err != nil {
		return model.Listing{}, fmt.Errorf("parking_spots: %w", err)
	}
	latitude, err := decimalPointer(value("latitude", "lat"))
	if err != nil {
		return model.Listing{}, fmt.Errorf("latitude: %w", err)
	}
	longitude, err := decimalPointer(value("longitude", "lon", "lng"))
	if err != nil {
		return model.Listing{}, fmt.Errorf("longitude: %w", err)
	}
	publishedAt, err := timePointer(value("published_at", "published"))
	if err != nil {
		return model.Listing{}, fmt.Errorf("published_at: %w", err)
	}

	source := text("source")
	if source == "" {
		source = defaultSource
	}
	collectedAt, err := timeValue(value("collected_at"))
	if err != nil {
		return model.Listing{}, fmt.Errorf("collected_at: %w", err)
	}
	if collectedAt.IsZero() {
		collectedAt = time.Now().UTC()
	}

	return model.Listing{
		Source: source, ExternalID: text("external_id", "id"), URL: text("url", "permalink"),
		Title: text("title", "name"), Description: text("description"),
		Operation: text("operation", "transaction_type"), PropertyType: text("property_type", "property"),
		Price: price, Currency: text("currency"), AreaM2: area, Bedrooms: bedrooms,
		Bathrooms: bathrooms, ParkingSpots: parking, Country: text("country"),
		Department: text("department", "state"), City: text("city"),
		Neighborhood: text("neighborhood", "district"), Address: text("address"),
		Latitude: latitude, Longitude: longitude, Images: stringSlice(value("images")),
		PublishedAt: publishedAt, CollectedAt: collectedAt,
	}, nil
}

func fieldKey(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func moneyValue(value any) (*int64, error) {
	if value == nil || strings.TrimSpace(stringValue(value)) == "" {
		return nil, nil
	}
	raw := strings.TrimSpace(stringValue(value))
	cleaned := strings.Map(func(character rune) rune {
		if character == '-' || character == '+' || character == '.' || character == ',' || unicode.IsDigit(character) {
			return character
		}
		return -1
	}, raw)
	if strings.ContainsAny(cleaned, ".,") {
		separator := strings.LastIndexAny(cleaned, ".,")
		fractionDigits := len(cleaned) - separator - 1
		if fractionDigits != 3 {
			return nil, fmt.Errorf("fractional amounts are not supported by int64 price field: %q", raw)
		}
	}
	cleaned = strings.ReplaceAll(cleaned, ".", "")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	result, err := strconv.ParseInt(cleaned, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid integer amount %q", raw)
	}
	return &result, nil
}

func decimalValue(value any) (*float64, error) {
	if value == nil || strings.TrimSpace(stringValue(value)) == "" {
		return nil, nil
	}
	raw := strings.TrimSpace(stringValue(value))
	if strings.ContainsAny(raw, "mM²") || strings.Contains(raw, " ") {
		return normalize.Area(raw), nil
	}
	raw = strings.ReplaceAll(raw, ",", ".")
	result, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid decimal %q", raw)
	}
	return &result, nil
}

func decimalPointer(value any) (*float64, error) { return decimalValue(value) }

func intPointer(value any) (*int, error) {
	if value == nil || strings.TrimSpace(stringValue(value)) == "" {
		return nil, nil
	}
	raw := stringValue(value)
	if parsed := normalize.Number(raw); parsed != nil {
		return parsed, nil
	}
	return nil, fmt.Errorf("invalid integer %q", raw)
}

func timeValue(value any) (time.Time, error) {
	if value == nil || strings.TrimSpace(stringValue(value)) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, stringValue(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("expected RFC3339 timestamp: %w", err)
	}
	return parsed.UTC(), nil
}

func timePointer(value any) (*time.Time, error) {
	parsed, err := timeValue(value)
	if err != nil || parsed.IsZero() {
		return nil, err
	}
	return &parsed, nil
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(stringValue(item)); text != "" {
				result = append(result, text)
			}
		}
		return result
	case string:
		var parsed []string
		if json.Unmarshal([]byte(typed), &parsed) == nil {
			return parsed
		}
		return splitImages(typed)
	default:
		return nil
	}
}

func splitImages(value string) []string {
	parts := strings.FieldsFunc(value, func(character rune) bool { return character == ',' || character == ';' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part); text != "" {
			result = append(result, text)
		}
	}
	return result
}
