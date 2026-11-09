package dccolombia

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/example/realestate-collector/internal/collector"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/normalize"
	"github.com/example/realestate-collector/internal/refresh"
	"github.com/example/realestate-collector/internal/sources/httpclient"
)

const DefaultEndpoint = "https://www.dccolombia.com.co/api/properties"

type Source struct {
	Client                        *httpclient.Client
	Endpoint                      string
	City, Operation, PropertyType string
}

func New(httpClient *http.Client, userAgent string, delay time.Duration) Source {
	client := httpclient.New(httpClient, userAgent, delay)
	client.SetSource("dccolombia")
	return Source{Client: client, Endpoint: DefaultEndpoint}
}

func (s Source) Name() string { return "dccolombia" }

type response struct {
	Properties *[]property `json:"properties"`
}

type property struct {
	Slug         string       `json:"slug"`
	ID           int64        `json:"id"`
	URL          string       `json:"url"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Operation    string       `json:"operation"`
	PropertyType string       `json:"property_type"`
	PriceSale    *json.Number `json:"price_sale"`
	PriceRent    *json.Number `json:"price_rent"`
	Currency     string       `json:"currency"`
	Area         *float64     `json:"area"`
	Bedrooms     *int         `json:"bedrooms"`
	Bathrooms    *int         `json:"bathrooms"`
	ParkingSpots *int         `json:"parking_spots"`
	City         string       `json:"city"`
	Neighborhood string       `json:"neighborhood"`
	Address      string       `json:"address"`
	Latitude     *float64     `json:"lat"`
	Longitude    *float64     `json:"lng"`
	Cover        string       `json:"cover"`
	Images       []string     `json:"images"`
	PublishedAt  *time.Time   `json:"published_at"`
}

func (s Source) Collect(ctx context.Context, limit int) ([]model.Listing, error) {
	listings, _, err := s.collect(ctx, limit)
	return listings, err
}

func (s Source) collect(ctx context.Context, limit int) ([]model.Listing, int, error) {
	if limit <= 0 {
		return nil, 0, fmt.Errorf("limit must be greater than zero")
	}
	if s.Client == nil {
		s.Client = httpclient.New(nil, "realestate-collector/0.1", 3*time.Second)
		s.Client.SetSource("dccolombia")
	}
	if s.Endpoint == "" {
		s.Endpoint = DefaultEndpoint
	}
	endpoint, err := url.Parse(s.Endpoint)
	if err != nil {
		return nil, 0, fmt.Errorf("parse D.C Colombia endpoint: %w", err)
	}
	query := endpoint.Query()
	if s.City != "" {
		query.Set("city", s.City)
	}
	operation, err := providerOperation(s.Operation)
	if err != nil {
		return nil, 0, err
	}
	if operation != "" {
		query.Set("operation", operation)
	}
	propertyType, err := providerPropertyType(s.PropertyType)
	if err != nil {
		return nil, 0, err
	}
	if propertyType != "" {
		query.Set("property_type", propertyType)
	}
	endpoint.RawQuery = query.Encode()
	slog.Debug("dccolombia_query", "city", s.City, "operation", operation, "property_type", propertyType)
	var page response
	if err := s.Client.GetJSON(ctx, endpoint.String(), &page); err != nil {
		return nil, 0, err
	}
	if page.Properties == nil {
		return nil, 0, fmt.Errorf("D.C Colombia response has no properties field")
	}
	properties := *page.Properties
	if len(properties) > limit {
		properties = properties[:limit]
	}
	listings := make([]model.Listing, 0, len(properties))
	rejected := 0
	for _, item := range properties {
		listing, err := toListing(item)
		if err != nil {
			rejected++
			continue
		}
		listings = append(listings, listing)
	}
	return listings, rejected, nil
}

func providerOperation(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case "SALE":
		return "sale", nil
	case "RENT":
		return "rent", nil
	default:
		return "", fmt.Errorf("D.C Colombia does not support operation %q", value)
	}
}

func providerPropertyType(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case "APARTMENT":
		return "apartment", nil
	case "HOUSE":
		return "house", nil
	case "LAND":
		return "lot", nil
	case "COMMERCIAL":
		return "commercial", nil
	default:
		return "", fmt.Errorf("D.C Colombia does not support property type %q", value)
	}
}

func (s Source) CollectForRefresh(ctx context.Context, limit int) (refresh.CollectResult, error) {
	listings, rejected, err := s.collect(ctx, limit)
	if err != nil {
		return refresh.CollectResult{}, err
	}
	result := refresh.CollectResult{Listings: listings, FetchedCount: len(listings), RejectedCount: rejected, PagesFetched: 1}
	if len(listings) == 0 {
		result.Complete = true
	} else {
		result.PartialReason = "provider_pagination_unknown"
	}
	return result, nil
}

func toListing(item property) (model.Listing, error) {
	externalID := item.Slug
	if externalID == "" && item.ID != 0 {
		externalID = strconv.FormatInt(item.ID, 10)
	}
	if externalID == "" {
		return model.Listing{}, fmt.Errorf("D.C Colombia property has no stable ID")
	}
	listingURL := item.URL
	if listingURL == "" {
		listingURL = "https://www.dccolombia.com.co/inmueble/" + url.PathEscape(externalID)
	}
	price := moneyNumber(item.PriceSale)
	if normalize.Operation(item.Operation) == "RENT" {
		price = moneyNumber(item.PriceRent)
	}
	currency := item.Currency
	if currency == "" && price != nil {
		currency = "COP"
	}
	return normalize.CleanListing(model.Listing{
		Source: "dccolombia", ExternalID: externalID, URL: listingURL,
		Title: item.Title, Description: item.Description, Operation: item.Operation,
		PropertyType: item.PropertyType, Price: price, Currency: currency,
		AreaM2: item.Area, Bedrooms: item.Bedrooms, Bathrooms: item.Bathrooms,
		ParkingSpots: item.ParkingSpots, Country: "Colombia", CountryCode: "CO", City: item.City,
		Neighborhood: item.Neighborhood, Address: item.Address, Latitude: item.Latitude, Longitude: item.Longitude,
		Images:      absoluteImages(append(item.Images, item.Cover)...),
		PublishedAt: item.PublishedAt, CollectedAt: time.Now().UTC(),
	}), nil
}

func absoluteImages(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		imageURL := value
		parsed, err := url.Parse(value)
		if err == nil && !parsed.IsAbs() {
			imageURL = "https://www.dccolombia.com.co/" + strings.TrimPrefix(value, "/")
		}
		if _, exists := seen[imageURL]; exists {
			continue
		}
		seen[imageURL] = struct{}{}
		result = append(result, imageURL)
	}
	return result
}

func moneyNumber(value *json.Number) *int64 {
	if value == nil || strings.TrimSpace(value.String()) == "" {
		return nil
	}
	raw := strings.TrimSpace(value.String())
	if strings.HasSuffix(raw, ".0") {
		raw = strings.TrimSuffix(raw, ".0")
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

var _ collector.Source = Source{}
