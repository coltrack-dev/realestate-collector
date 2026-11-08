package gallito

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/realestate-collector/internal/collector"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/normalize"
)

const DefaultEndpoint = "https://inmuebles.elpais.com.uy/api/public-mcp"

type Source struct {
	HTTP          *http.Client
	Endpoint      string
	UserAgent     string
	Delay         time.Duration
	City          string
	Department    string
	Operation     string
	PropertyTypes []string

	limiter *rateLimiter
}

type rateLimiter struct {
	mu          sync.Mutex
	lastRequest time.Time
}

func New(httpClient *http.Client, userAgent string, delay time.Duration) Source {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if delay <= 0 {
		delay = 3 * time.Second
	}
	return Source{HTTP: httpClient, Endpoint: DefaultEndpoint, UserAgent: userAgent, Delay: delay, limiter: &rateLimiter{}}
}

func (s Source) Name() string { return "gallito-uruguay" }

type rpcEnvelope struct {
	Result *struct {
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type searchArguments struct {
	Operation       string   `json:"operation,omitempty"`
	City            string   `json:"city,omitempty"`
	Department      string   `json:"department,omitempty"`
	PropertyTypes   []string `json:"propertyTypes,omitempty"`
	IncludeProjects bool     `json:"includeProjects"`
	IncludeListings bool     `json:"includeListings"`
	Sort            string   `json:"sort,omitempty"`
	Language        string   `json:"language"`
}

type moreResultsArguments struct {
	SearchID string `json:"searchId"`
	Page     int    `json:"page"`
}

type getListingArguments struct {
	ID string `json:"id"`
}

type searchResponse struct {
	SearchID   string    `json:"searchId"`
	Page       int       `json:"page"`
	TotalPages int       `json:"totalPages"`
	Total      int       `json:"total"`
	Listings   []summary `json:"listings"`
}

type summary struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Operation       string    `json:"operation"`
	PropertyType    string    `json:"propertyType"`
	Price           *price    `json:"price"`
	Neighbourhood   string    `json:"neighbourhood"`
	City            string    `json:"city"`
	Province        string    `json:"province"`
	Bedrooms        *int      `json:"bedrooms"`
	Bathrooms       *int      `json:"bathrooms"`
	AreaM2          *float64  `json:"areaM2"`
	Photos          []string  `json:"photos"`
	Location        *location `json:"location"`
	URL             string    `json:"url"`
	DescriptionText string    `json:"descriptionText"`
	Address         string    `json:"address"`
	LandAreaM2      *float64  `json:"landAreaM2"`
	PublicRef       string    `json:"publicRef"`
}

type detail = summary

type price struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type location struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
}

func (s Source) Collect(ctx context.Context, limit int) ([]model.Listing, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be greater than zero")
	}
	if s.Endpoint == "" {
		s.Endpoint = DefaultEndpoint
	}
	if s.UserAgent == "" {
		s.UserAgent = "realestate-collector/0.1"
	}
	if s.HTTP == nil {
		s.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	arguments := searchArguments{Operation: s.Operation, City: s.City, Department: s.Department,
		PropertyTypes: s.PropertyTypes, IncludeProjects: false, IncludeListings: true, Sort: "newest", Language: "es"}
	var page searchResponse
	if err := s.call(ctx, "search_listings", arguments, &page); err != nil {
		return nil, err
	}
	listings := make([]model.Listing, 0, min(limit, 100))
	for currentPage := 1; ; currentPage++ {
		for _, item := range page.Listings {
			listing, err := toListing(item)
			if err != nil {
				continue
			}
			listings = append(listings, listing)
			if len(listings) == limit {
				return listings, nil
			}
		}
		if page.SearchID == "" || currentPage >= page.TotalPages || currentPage >= 10 {
			break
		}
		if err := s.call(ctx, "more_results", moreResultsArguments{SearchID: page.SearchID, Page: currentPage + 1}, &page); err != nil {
			return nil, err
		}
	}
	return listings, nil
}

func (s Source) FetchDetails(ctx context.Context, externalID string) (model.Listing, error) {
	if strings.TrimSpace(externalID) == "" {
		return model.Listing{}, fmt.Errorf("Gallito listing ID is required")
	}
	var listing detail
	if err := s.call(ctx, "get_listing", getListingArguments{ID: externalID}, &listing); err != nil {
		return model.Listing{}, err
	}
	return toListing(listing)
}

func toListing(item summary) (model.Listing, error) {
	if item.ID == "" || item.URL == "" {
		return model.Listing{}, fmt.Errorf("Gallito listing has no ID or URL")
	}
	var area *float64
	if item.AreaM2 != nil && *item.AreaM2 > 0 {
		area = item.AreaM2
	}
	var latitude, longitude *float64
	if item.Location != nil {
		latitude = &item.Location.Latitude
		longitude = &item.Location.Longitude
	}
	return normalize.CleanListing(model.Listing{
		Source: "gallito-uruguay", ExternalID: item.ID, URL: item.URL, Title: item.Title,
		Description: item.DescriptionText, Operation: item.Operation, PropertyType: item.PropertyType,
		Price: priceAmount(item.Price), Currency: priceCurrency(item.Price), AreaM2: area,
		Bedrooms: item.Bedrooms, Bathrooms: item.Bathrooms, Country: "Uruguay", CountryCode: "UY",
		Department: item.Province, City: item.City, Neighborhood: item.Neighbourhood,
		Address: item.Address, Latitude: latitude, Longitude: longitude, Images: uniqueStrings(item.Photos),
		CollectedAt: time.Now().UTC(), DetailsStatus: "NOT_LOADED",
	}), nil
}

func priceAmount(value *price) *int64 {
	if value == nil {
		return nil
	}
	return &value.Amount
}

func priceCurrency(value *price) string {
	if value == nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(value.Currency))
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
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

func (s Source) call(ctx context.Context, method string, arguments any, target any) error {
	if err := s.wait(ctx); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": method, "arguments": arguments}})
	if err != nil {
		return fmt.Errorf("encode Gallito MCP request: %w", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Endpoint, strings.NewReader(string(body)))
		if err != nil {
			return fmt.Errorf("create Gallito MCP request: %w", err)
		}
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("User-Agent", s.UserAgent)
		response, err := s.HTTP.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt < 2 {
				if err := retryWait(ctx, attempt, ""); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("Gallito MCP %s request: %w", method, err)
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 10<<20))
		response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Gallito MCP %s response: %w", method, readErr)
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			if attempt < 2 {
				if err := retryWait(ctx, attempt, response.Header.Get("Retry-After")); err != nil {
					return err
				}
				continue
			}
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("Gallito MCP %s: access denied with HTTP %d", method, response.StatusCode)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("Gallito MCP %s: HTTP %d: %s", method, response.StatusCode, strings.TrimSpace(string(responseBody)))
		}
		var envelope rpcEnvelope
		if err := json.Unmarshal(responseBody, &envelope); err != nil {
			return fmt.Errorf("decode Gallito MCP %s response: %w", method, err)
		}
		if envelope.Error != nil {
			return fmt.Errorf("Gallito MCP %s error %d: %s", method, envelope.Error.Code, envelope.Error.Message)
		}
		if envelope.Result == nil {
			return fmt.Errorf("Gallito MCP %s returned no result", method)
		}
		if len(envelope.Result.StructuredContent) > 0 && string(envelope.Result.StructuredContent) != "null" {
			if err := json.Unmarshal(envelope.Result.StructuredContent, target); err == nil {
				return nil
			}
		}
		for _, content := range envelope.Result.Content {
			if content.Text == "" {
				continue
			}
			if err := json.Unmarshal([]byte(content.Text), target); err == nil {
				return nil
			}
		}
		return fmt.Errorf("Gallito MCP %s returned no structured data", method)
	}
	return fmt.Errorf("Gallito MCP %s: request attempts exhausted", method)
}

func retryWait(ctx context.Context, attempt int, retryAfter string) error {
	delay := time.Duration(attempt+1) * time.Second
	if seconds, err := time.ParseDuration(strings.TrimSpace(retryAfter) + "s"); err == nil && strings.TrimSpace(retryAfter) != "" {
		delay = seconds
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s Source) wait(ctx context.Context) error {
	if s.limiter == nil {
		return nil
	}
	s.limiter.mu.Lock()
	waitFor := s.Delay - time.Since(s.limiter.lastRequest)
	if waitFor < 0 {
		waitFor = 0
	}
	s.limiter.lastRequest = time.Now()
	s.limiter.mu.Unlock()
	if waitFor == 0 {
		return nil
	}
	timer := time.NewTimer(waitFor)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ collector.Source = Source{}
var _ collector.DetailSource = Source{}
