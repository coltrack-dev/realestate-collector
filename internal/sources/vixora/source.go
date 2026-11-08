package vixora

import (
	"context"
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

const DefaultEndpoint = "https://api.vixoraai.com/public/v1/properties"

type Source struct {
	Client       *httpclient.Client
	Endpoint     string
	Operation    string
	PropertyType string
	Municipality string
}

func New(httpClient *http.Client, userAgent string, delay time.Duration) Source {
	return Source{Client: httpclient.New(httpClient, userAgent, delay), Endpoint: DefaultEndpoint}
}

func (s Source) Name() string { return "vixora" }

type response struct {
	Data       []property `json:"data"`
	Pagination struct {
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		TotalPages int `json:"total_pages"`
		Next       any `json:"next"`
	} `json:"pagination"`
}

type property struct {
	Code      string `json:"code"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Operation string `json:"operation"`
	Status    string `json:"status"`
	Price     *struct {
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
	} `json:"price"`
	Location struct {
		Department  string `json:"department"`
		Municipality string `json:"municipality"`
		Vereda      string `json:"vereda"`
		Point       *struct {
			Latitude  float64 `json:"lat"`
			Longitude float64 `json:"lon"`
		} `json:"point"`
	} `json:"location"`
	Areas struct {
		BuiltM2 *float64 `json:"built_m2"`
		LotM2   *float64 `json:"lot_m2"`
	} `json:"areas"`
	Bedrooms   *int      `json:"bedrooms"`
	Bathrooms  *int      `json:"bathrooms"`
	Image      string    `json:"image"`
	URL        string    `json:"url"`
	PublishedAt *time.Time `json:"published_at"`
	Description string `json:"description"`
	Images     []string  `json:"images"`
}

func (s Source) Collect(ctx context.Context, limit int) ([]model.Listing, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be greater than zero")
	}
	if s.Client == nil {
		s.Client = httpclient.New(nil, "realestate-collector/0.1", 3*time.Second)
	}
	if s.Endpoint == "" {
		s.Endpoint = DefaultEndpoint
	}
	pageSize := limit
	if pageSize > 50 {
		pageSize = 50
	}
	var listings []model.Listing
	for page := 1; len(listings) < limit; page++ {
		endpoint, err := url.Parse(s.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("parse VIXORA endpoint: %w", err)
		}
		query := endpoint.Query()
		query.Set("page", strconv.Itoa(page))
		query.Set("page_size", strconv.Itoa(pageSize))
		if s.Operation != "" {
			query.Set("operation", s.Operation)
		}
		if s.PropertyType != "" {
			query.Set("type", s.PropertyType)
		}
		if s.Municipality != "" {
			query.Set("municipality", s.Municipality)
		}
		endpoint.RawQuery = query.Encode()
		var pageResponse response
		if err := s.Client.GetJSON(ctx, endpoint.String(), &pageResponse); err != nil {
			return nil, err
		}
		for _, item := range pageResponse.Data {
			listing, err := toListing(item)
			if err != nil {
				continue
			}
			listings = append(listings, listing)
			if len(listings) == limit {
				return listings, nil
			}
		}
		if len(pageResponse.Data) == 0 || (pageResponse.Pagination.TotalPages > 0 && page >= pageResponse.Pagination.TotalPages) {
			break
		}
	}
	return listings, nil
}

func toListing(item property) (model.Listing, error) {
	if item.Code == "" || item.URL == "" {
		return model.Listing{}, fmt.Errorf("VIXORA property has no code or URL")
	}
	var price *int64
	currency := ""
	if item.Price != nil {
		price = &item.Price.Amount
		currency = item.Price.Currency
	}
	images := append([]string(nil), item.Images...)
	if item.Image != "" {
		images = append(images, item.Image)
	}
	var latitude, longitude *float64
	if item.Location.Point != nil {
		latitude = &item.Location.Point.Latitude
		longitude = &item.Location.Point.Longitude
	}
	area := item.Areas.BuiltM2
	if area == nil {
		area = item.Areas.LotM2
	}
	return normalize.CleanListing(model.Listing{
		Source: "vixora", ExternalID: item.Code, URL: item.URL, Title: item.Title,
		Description: item.Description, Operation: item.Operation, PropertyType: item.Type,
		Price: price, Currency: currency, AreaM2: area, Bedrooms: item.Bedrooms,
		Bathrooms: item.Bathrooms, Country: "Colombia", Department: item.Location.Department,
		City: item.Location.Municipality, Neighborhood: item.Location.Vereda,
		Latitude: latitude, Longitude: longitude, Images: uniqueStrings(images),
		PublishedAt: item.PublishedAt, CollectedAt: time.Now().UTC(),
	}), nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

var _ collector.Source = Source{}
