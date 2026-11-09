package vixora

import (
	"context"
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

const DefaultEndpoint = "https://api.vixoraai.com/public/v1/properties"

type Source struct {
	Client       *httpclient.Client
	Endpoint     string
	Operation    string
	PropertyType string
	Municipality string
}

// VIXORA exposes Spanish, lower-case filter values. These mappings are kept
// here instead of leaking provider-specific values into the coordinator.
var operationValues = map[string]string{
	"SALE": "venta",
	"RENT": "arriendo",
}

var propertyTypeValues = map[string]string{
	"APARTMENT":  "apartamento",
	"HOUSE":      "casa",
	"LAND":       "lote",
	"COMMERCIAL": "local",
}

// This is a deliberately conservative local cache of municipalities observed
// in the provider's public locations/inventory documentation and fixtures.
// Unknown municipalities are rejected rather than causing a broad provider
// query. Refresh this list only from the provider's /locations contract.
var supportedMunicipalities = map[string]string{
	"el penol":           "el-penol",
	"el retiro":          "el-retiro",
	"guarne":             "guarne",
	"guatape":            "guatape",
	"la ceja":            "la-ceja",
	"marinilla":          "marinilla",
	"rionegro":           "rionegro",
	"san vicente ferrer": "san-vicente-ferrer",
}

func SupportedMunicipalities() []string {
	return []string{"El Peñol", "El Retiro", "Guarne", "Guatapé", "La Ceja", "Marinilla", "Rionegro", "San Vicente Ferrer"}
}

func providerFilters(operation, propertyType, municipality string) (string, string, string, string) {
	if strings.TrimSpace(operation) == "" && strings.TrimSpace(propertyType) == "" && strings.TrimSpace(municipality) == "" {
		return "", "", "", ""
	}
	apiOperation := ""
	if value := strings.ToUpper(strings.TrimSpace(operation)); value != "" {
		mapped, ok := operationValues[value]
		if !ok {
			return "", "", "", "unsupported_operation"
		}
		apiOperation = mapped
	}
	apiPropertyType := ""
	if value := strings.ToUpper(strings.TrimSpace(propertyType)); value != "" {
		mapped, ok := propertyTypeValues[value]
		if !ok {
			return "", "", "", "unsupported_property_type"
		}
		apiPropertyType = mapped
	}
	apiMunicipality, municipalityOK := normalizeMunicipality(municipality)
	if !municipalityOK {
		return "", "", "", "unsupported_municipality"
	}
	return apiOperation, apiPropertyType, apiMunicipality, ""
}

func normalizeMunicipality(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u").Replace(value)
	value = strings.Join(strings.Fields(strings.ReplaceAll(value, "-", " ")), " ")
	municipality, ok := supportedMunicipalities[value]
	return municipality, ok
}

func New(httpClient *http.Client, userAgent string, delay time.Duration) Source {
	client := httpclient.New(httpClient, userAgent, delay)
	client.SetSource("vixora")
	return Source{Client: client, Endpoint: DefaultEndpoint}
}

func (s Source) Name() string { return "vixora" }

type response struct {
	Data       []property `json:"data"`
	Pagination struct {
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		TotalPages int `json:"total_pages"`
		Total      int `json:"total"`
		Next       any `json:"next"`
	} `json:"pagination"`
}

type property struct {
	Code      string `json:"code"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Operation string `json:"operation"`
	Status    string `json:"status"`
	Price     *struct {
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
	} `json:"price"`
	Location struct {
		Department   string `json:"department"`
		Municipality string `json:"municipality"`
		Vereda       string `json:"vereda"`
		Point        *struct {
			Latitude  float64 `json:"lat"`
			Longitude float64 `json:"lon"`
		} `json:"point"`
	} `json:"location"`
	Areas struct {
		BuiltM2 *float64 `json:"built_m2"`
		LotM2   *float64 `json:"lot_m2"`
	} `json:"areas"`
	Bedrooms    *int       `json:"bedrooms"`
	Bathrooms   *int       `json:"bathrooms"`
	Image       string     `json:"image"`
	URL         string     `json:"url"`
	PublishedAt *time.Time `json:"published_at"`
	Description string     `json:"description"`
	Images      []string   `json:"images"`
}

func (s Source) Collect(ctx context.Context, limit int) ([]model.Listing, error) {
	result, err := s.CollectForRefresh(ctx, limit)
	return result.Listings, err
}

func (s Source) CollectForRefresh(ctx context.Context, limit int) (refresh.CollectResult, error) {
	if limit <= 0 {
		return refresh.CollectResult{}, fmt.Errorf("limit must be greater than zero")
	}
	if s.Client == nil {
		s.Client = httpclient.New(nil, "realestate-collector/0.1", 3*time.Second)
		s.Client.SetSource("vixora")
	}
	if s.Endpoint == "" {
		s.Endpoint = DefaultEndpoint
	}
	apiOperation, apiPropertyType, apiMunicipality, reason := providerFilters(s.Operation, s.PropertyType, s.Municipality)
	if reason != "" {
		slog.Debug("vixora_refresh_skipped", "reason", reason, "operation", strings.ToUpper(strings.TrimSpace(s.Operation)), "property_type", strings.ToUpper(strings.TrimSpace(s.PropertyType)), "municipality", strings.TrimSpace(s.Municipality))
		return refresh.CollectResult{Skipped: true, SkipReason: reason, Complete: true}, nil
	}
	pageSize := limit
	if pageSize > 50 {
		pageSize = 50
	}
	var listings []model.Listing
	pagesFetched := 0
	complete := false
	partialReason := ""
	var totalCount *int
	rejected := 0
	for page := 1; len(listings) < limit; page++ {
		endpoint, err := url.Parse(s.Endpoint)
		if err != nil {
			return refresh.CollectResult{}, fmt.Errorf("parse VIXORA endpoint: %w", err)
		}
		query := endpoint.Query()
		query.Set("page", strconv.Itoa(page))
		query.Set("page_size", strconv.Itoa(pageSize))
		if apiOperation != "" {
			query.Set("operation", apiOperation)
		}
		if apiPropertyType != "" {
			query.Set("type", apiPropertyType)
		}
		if apiMunicipality != "" {
			query.Set("municipality", apiMunicipality)
		}
		endpoint.RawQuery = query.Encode()
		slog.Debug("vixora_query", "operation", apiOperation, "type", apiPropertyType, "municipality", apiMunicipality, "page", page, "page_size", pageSize)
		var pageResponse response
		if err := s.Client.GetJSON(ctx, endpoint.String(), &pageResponse); err != nil {
			return refresh.CollectResult{}, err
		}
		pagesFetched++
		if pageResponse.Pagination.Total > 0 {
			total := pageResponse.Pagination.Total
			totalCount = &total
		}
		for _, item := range pageResponse.Data {
			listing, err := toListing(item)
			if err != nil {
				rejected++
				continue
			}
			listings = append(listings, listing)
			if len(listings) == limit {
				if pageResponse.Pagination.TotalPages > 0 && page >= pageResponse.Pagination.TotalPages {
					complete = true
				} else {
					partialReason = "limit_reached"
				}
				return refresh.CollectResult{Listings: listings, FetchedCount: len(listings), RejectedCount: rejected, TotalCount: totalCount, PagesFetched: pagesFetched, Complete: complete, PartialReason: partialReason}, nil
			}
		}
		if len(pageResponse.Data) == 0 || (pageResponse.Pagination.TotalPages > 0 && page >= pageResponse.Pagination.TotalPages) {
			complete = true
			break
		}
	}
	if !complete {
		partialReason = "pagination_exhaustion_not_proven"
	}
	return refresh.CollectResult{Listings: listings, FetchedCount: len(listings), RejectedCount: rejected, TotalCount: totalCount, PagesFetched: pagesFetched, Complete: complete, PartialReason: partialReason}, nil
}

func (s Source) FetchDetails(ctx context.Context, externalID string) (model.Listing, error) {
	if strings.TrimSpace(externalID) == "" {
		return model.Listing{}, fmt.Errorf("VIXORA external ID is required")
	}
	if s.Client == nil {
		s.Client = httpclient.New(nil, "realestate-collector/0.1", 3*time.Second)
		s.Client.SetSource("vixora")
	}
	if s.Endpoint == "" {
		s.Endpoint = DefaultEndpoint
	}
	endpoint, err := url.Parse(strings.TrimRight(s.Endpoint, "/") + "/" + url.PathEscape(externalID))
	if err != nil {
		return model.Listing{}, fmt.Errorf("parse VIXORA detail endpoint: %w", err)
	}
	var detail property
	if err := s.Client.GetJSON(ctx, endpoint.String(), &detail); err != nil {
		return model.Listing{}, err
	}
	if detail.Code == "" {
		return model.Listing{}, fmt.Errorf("VIXORA detail response has no code")
	}
	return toListing(detail)
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
		Bathrooms: item.Bathrooms, Country: "Colombia", CountryCode: "CO", Department: item.Location.Department,
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
var _ collector.DetailSource = Source{}
