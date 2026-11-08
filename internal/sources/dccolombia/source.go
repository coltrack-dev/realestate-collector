package dccolombia

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/example/realestate-collector/internal/collector"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/normalize"
	"github.com/example/realestate-collector/internal/sources/httpclient"
)

const DefaultEndpoint = "https://www.dccolombia.com.co/api/properties"

type Source struct {
	Client *httpclient.Client
	Endpoint string
}

func New(httpClient *http.Client, userAgent string, delay time.Duration) Source {
	return Source{Client: httpclient.New(httpClient, userAgent, delay), Endpoint: DefaultEndpoint}
}

func (s Source) Name() string { return "dccolombia" }

type response struct { Properties []property `json:"properties"` }

type property struct {
	Slug string `json:"slug"`
	ID int64 `json:"id"`
	URL string `json:"url"`
	Title string `json:"title"`
	Description string `json:"description"`
	Operation string `json:"operation"`
	PropertyType string `json:"property_type"`
	PriceSale *json.Number `json:"price_sale"`
	PriceRent *json.Number `json:"price_rent"`
	Currency string `json:"currency"`
	Area *float64 `json:"area"`
	Bedrooms *int `json:"bedrooms"`
	Bathrooms *int `json:"bathrooms"`
	ParkingSpots *int `json:"parking_spots"`
	City string `json:"city"`
	Neighborhood string `json:"neighborhood"`
	Address string `json:"address"`
	Latitude *float64 `json:"lat"`
	Longitude *float64 `json:"lng"`
	Cover string `json:"cover"`
	Images []string `json:"images"`
	PublishedAt *time.Time `json:"published_at"`
}

func (s Source) Collect(ctx context.Context, limit int) ([]model.Listing, error) {
	if limit <= 0 { return nil, fmt.Errorf("limit must be greater than zero") }
	if s.Client == nil { s.Client = httpclient.New(nil, "realestate-collector/0.1", 3*time.Second) }
	if s.Endpoint == "" { s.Endpoint = DefaultEndpoint }
	endpoint, err := url.Parse(s.Endpoint)
	if err != nil { return nil, fmt.Errorf("parse D.C Colombia endpoint: %w", err) }
	var page response
	if err := s.Client.GetJSON(ctx, endpoint.String(), &page); err != nil { return nil, err }
	if len(page.Properties) > limit { page.Properties = page.Properties[:limit] }
	listings := make([]model.Listing, 0, len(page.Properties))
	for _, item := range page.Properties {
		listing, err := toListing(item)
		if err != nil { continue }
		listings = append(listings, listing)
	}
	return listings, nil
}

func toListing(item property) (model.Listing, error) {
	externalID := item.Slug
	if externalID == "" && item.ID != 0 { externalID = strconv.FormatInt(item.ID, 10) }
	if externalID == "" { return model.Listing{}, fmt.Errorf("D.C Colombia property has no stable ID") }
	listingURL := item.URL
	if listingURL == "" { listingURL = "https://www.dccolombia.com.co/inmueble/" + url.PathEscape(externalID) }
	price := moneyNumber(item.PriceSale)
	if normalize.Operation(item.Operation) == "RENT" { price = moneyNumber(item.PriceRent) }
	currency := item.Currency
	if currency == "" && price != nil { currency = "COP" }
	return normalize.CleanListing(model.Listing{
		Source: "dccolombia", ExternalID: externalID, URL: listingURL,
		Title: item.Title, Description: item.Description, Operation: item.Operation,
		PropertyType: item.PropertyType, Price: price, Currency: currency,
		AreaM2: item.Area, Bedrooms: item.Bedrooms, Bathrooms: item.Bathrooms,
		ParkingSpots: item.ParkingSpots, Country: "Colombia", City: item.City,
		Neighborhood: item.Neighborhood, Address: item.Address, Latitude: item.Latitude, Longitude: item.Longitude,
		Images: absoluteImages(append(item.Images, item.Cover)...),
		PublishedAt: item.PublishedAt, CollectedAt: time.Now().UTC(),
	}), nil
}

func absoluteImages(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" { continue }
		parsed, err := url.Parse(value)
		if err == nil && parsed.IsAbs() { result = append(result, value); continue }
		result = append(result, "https://www.dccolombia.com.co/"+strings.TrimPrefix(value, "/"))
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
