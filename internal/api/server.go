package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/realestate-collector/internal/geo"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/refresh"
)

const (
	DefaultPageSize = 50
	MaxPageSize     = 100
	RequestTimeout  = 5 * time.Second
)

var ErrNotFound = errors.New("listing not found")

type ListingFilter struct {
	Source, CountryCode, City, Operation, PropertyType string
	MinPrice, MaxPrice                                 *int64
	MinArea, MaxArea                                   *float64
	MinLongitude, MaxLongitude                         *float64
	MinLatitude, MaxLatitude                           *float64
	Limit, Offset                                      int
}

type Snapshot struct {
	ID          int64     `json:"id"`
	Price       *int64    `json:"price"`
	Currency    *string   `json:"currency"`
	AreaM2      *float64  `json:"area_m2"`
	ContentHash string    `json:"content_hash"`
	CollectedAt time.Time `json:"collected_at"`
}

type ListingRecord struct {
	ID          int64
	Listing     model.Listing
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

type Stats struct {
	Total         int64            `json:"total"`
	BySource      map[string]int64 `json:"by_source"`
	ByOperation   map[string]int64 `json:"by_operation"`
	LastUpdatedAt *time.Time       `json:"last_updated_at"`
}

type Repository interface {
	Ping(context.Context) error
	List(context.Context, ListingFilter) ([]ListingRecord, int64, error)
	Get(context.Context, int64) (ListingRecord, error)
	History(context.Context, int64) ([]Snapshot, error)
	Stats(context.Context) (Stats, error)
}

type Server struct {
	Repository Repository
	Details    *DetailsService
	Refresh    *refresh.Coordinator
	Geo        geo.Resolver
}

func NewServer(repository Repository, details ...*DetailsService) http.Handler {
	var detail *DetailsService
	if len(details) > 0 {
		detail = details[0]
	}
	return newServer(repository, detail, nil, nil)
}

func NewServerWithRefresh(repository Repository, details *DetailsService, coordinator *refresh.Coordinator) http.Handler {
	return newServer(repository, details, coordinator, nil)
}

func NewServerWithGeo(repository Repository, details *DetailsService, coordinator *refresh.Coordinator, resolver geo.Resolver) http.Handler {
	return newServer(repository, details, coordinator, resolver)
}

func newServer(repository Repository, details *DetailsService, coordinator *refresh.Coordinator, resolver geo.Resolver) http.Handler {
	server := &Server{Repository: repository, Details: details, Refresh: coordinator, Geo: resolver}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.health)
	mux.HandleFunc("/api/v1/listings", server.listings)
	mux.HandleFunc("/api/v1/listings/", server.listingByID)
	mux.HandleFunc("/api/v1/stats", server.stats)
	mux.HandleFunc("/api/v1/source-refresh", server.sourceRefresh)
	mux.HandleFunc("/api/v1/source-refresh/", server.sourceRefreshByID)
	mux.HandleFunc("/api/v1/geo/resolve", server.resolveGeo)
	mux.HandleFunc("/api/v1/geo/resolve-viewport", server.resolveGeoViewport)
	mux.HandleFunc("/api/v1/source-refresh/viewport", server.sourceRefreshViewport)
	return loggingMiddleware(mux)
}

func (s *Server) resolveGeo(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer)
		return
	}
	if s.Geo == nil {
		writeJSON(writer, http.StatusOK, geo.Result{Resolution: geo.Unknown})
		return
	}
	longitude, err := strconv.ParseFloat(request.URL.Query().Get("longitude"), 64)
	if err != nil || math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 {
		writeError(writer, http.StatusBadRequest, "longitude is invalid")
		return
	}
	latitude, err := strconv.ParseFloat(request.URL.Query().Get("latitude"), 64)
	if err != nil || math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 {
		writeError(writer, http.StatusBadRequest, "latitude is invalid")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), RequestTimeout)
	defer cancel()
	result, err := s.Geo.Resolve(ctx, geo.Point{Longitude: longitude, Latitude: latitude})
	if err != nil {
		slog.Warn("geo_resolve_failed", "error", err)
		writeError(writer, http.StatusInternalServerError, "geographic resolution failed")
		return
	}
	if s.Refresh != nil && result.CountryCode != "" {
		result.AvailableSources = s.Refresh.AvailableSources(result.CountryCode, result.Municipality)
	}
	writeJSON(writer, http.StatusOK, result)
}

func (s *Server) resolveGeoViewport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer)
		return
	}
	resolver, ok := s.Geo.(geo.ViewportResolver)
	if !ok {
		writeJSON(writer, http.StatusOK, geo.ViewportResult{Municipalities: []geo.ViewportMunicipality{}})
		return
	}
	var payload struct {
		BBox geo.BBox `json:"bbox"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10)).Decode(&payload); err != nil || !payload.BBox.Valid() {
		writeError(writer, http.StatusBadRequest, "bbox coordinates are invalid")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), RequestTimeout)
	defer cancel()
	result, err := resolver.ResolveViewport(ctx, payload.BBox)
	if err != nil {
		slog.Warn("geo_viewport_resolve_failed", "error", err)
		writeError(writer, http.StatusInternalServerError, "geographic resolution failed")
		return
	}
	if s.Refresh != nil {
		for i := range result.Municipalities {
			result.Municipalities[i].AvailableSources = s.Refresh.AvailableSources(result.Municipalities[i].CountryCode, result.Municipalities[i].Municipality)
		}
	}
	writeJSON(writer, http.StatusOK, result)
}

type sourceRefreshRequest struct {
	CountryCode  string         `json:"country_code"`
	City         string         `json:"city"`
	Department   string         `json:"department"`
	Bounds       refresh.Bounds `json:"bbox"`
	Operation    string         `json:"operation"`
	PropertyType string         `json:"property_type"`
}

type viewportSourceRefreshRequest struct {
	CountryCode  string   `json:"country_code"`
	Bounds       geo.BBox `json:"bbox"`
	Operation    string   `json:"operation"`
	PropertyType string   `json:"property_type"`
	MaxQueries   int      `json:"max_queries"`
}

func (s *Server) sourceRefreshViewport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer)
		return
	}
	if s.Refresh == nil {
		writeJSON(writer, http.StatusNotImplemented, refresh.ViewportPlan{})
		return
	}
	if !s.Refresh.Enabled() {
		writeJSON(writer, http.StatusForbidden, refresh.ViewportPlan{})
		return
	}
	var payload viewportSourceRefreshRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10)).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid viewport refresh request")
		return
	}
	if err := validateViewportRefresh(payload); err != nil {
		slog.Warn("source_refresh_rejected", "reason", err.Error())
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	resolver, ok := s.Geo.(geo.ViewportResolver)
	if !ok {
		writeError(writer, http.StatusNotImplemented, "viewport geographic resolver is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), RequestTimeout)
	defer cancel()
	resolved, err := resolver.ResolveViewport(ctx, payload.Bounds)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "geographic resolution failed")
		return
	}
	areas := make([]refresh.ViewportArea, 0, len(resolved.Municipalities))
	center := ""
	for _, area := range resolved.Municipalities {
		if payload.CountryCode != "" && !strings.EqualFold(payload.CountryCode, area.CountryCode) {
			continue
		}
		areas = append(areas, refresh.ViewportArea{CountryCode: area.CountryCode, Department: area.Department, Municipality: area.Municipality, DIVIPOLA: area.DIVIPOLA, OverlapArea: area.OverlapAreaM2, OverlapRatio: area.OverlapRatio})
	}
	if len(areas) > 0 {
		centerPoint := geo.Point{Longitude: (payload.Bounds.MinLongitude + payload.Bounds.MaxLongitude) / 2, Latitude: (payload.Bounds.MinLatitude + payload.Bounds.MaxLatitude) / 2}
		if centerResult, resolveErr := s.Geo.Resolve(ctx, centerPoint); resolveErr == nil {
			center = centerResult.Municipality
		}
	}
	clientKey := request.RemoteAddr
	if host, _, splitErr := net.SplitHostPort(clientKey); splitErr == nil {
		clientKey = host
	}
	plan := s.Refresh.RequestViewport(refresh.Request{CountryCode: payload.CountryCode, Bounds: refresh.Bounds{MinLongitude: payload.Bounds.MinLongitude, MaxLongitude: payload.Bounds.MaxLongitude, MinLatitude: payload.Bounds.MinLatitude, MaxLatitude: payload.Bounds.MaxLatitude}, Operation: payload.Operation, PropertyType: payload.PropertyType}, areas, center, payload.MaxQueries, clientKey)
	writeJSON(writer, http.StatusAccepted, plan)
}

func validateViewportRefresh(payload viewportSourceRefreshRequest) error {
	if payload.CountryCode != "CO" && payload.CountryCode != "UY" {
		return errors.New("country_code must be CO or UY")
	}
	if payload.Operation != "" && payload.Operation != "SALE" && payload.Operation != "RENT" {
		return errors.New("operation must be SALE, RENT, or empty for ALL")
	}
	if payload.PropertyType != "" {
		switch payload.PropertyType {
		case "APARTMENT", "HOUSE", "LAND", "COMMERCIAL":
		default:
			return errors.New("property_type is not supported or empty for ALL")
		}
	}
	b := payload.Bounds
	if !b.Valid() || b.MinLongitude < -180 || b.MaxLongitude > 180 || b.MinLatitude < -90 || b.MaxLatitude > 90 || b.MaxLongitude-b.MinLongitude > 15 || b.MaxLatitude-b.MinLatitude > 15 {
		return errors.New("bbox coordinates are invalid or too large")
	}
	if payload.MaxQueries < 0 || payload.MaxQueries > 3 {
		return errors.New("max_queries must be between 0 and 3")
	}
	return nil
}

func (s *Server) sourceRefresh(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer)
		return
	}
	if s.Refresh == nil {
		slog.Warn("source_refresh_rejected", "reason", "coordinator_not_configured")
		writeJSON(writer, http.StatusNotImplemented, refresh.Result{Status: refresh.StatusUnsupported})
		return
	}
	if !s.Refresh.Enabled() {
		slog.Warn("source_refresh_rejected", "reason", "refresh_disabled")
		writeJSON(writer, http.StatusForbidden, refresh.Result{Status: refresh.StatusUnsupported})
		return
	}
	var payload sourceRefreshRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10)).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid refresh request")
		return
	}
	if err := validateSourceRefresh(payload); err != nil {
		slog.Warn("source_refresh_rejected", "reason", err.Error())
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	clientKey := request.RemoteAddr
	if host, _, err := net.SplitHostPort(clientKey); err == nil {
		clientKey = host
	}
	slog.Info("source_refresh_requested", "country_code", payload.CountryCode, "city", payload.City, "department", payload.Department, "operation", payload.Operation, "property_type", payload.PropertyType)
	result := s.Refresh.Request(refresh.Request{CountryCode: payload.CountryCode, City: payload.City, Department: payload.Department, Bounds: payload.Bounds, Operation: payload.Operation, PropertyType: payload.PropertyType}, clientKey)
	if result.Status == refresh.StatusUnsupported || result.Status == refresh.StatusSkipped || result.Status == refresh.StatusCooldown {
		slog.Debug("source_refresh_rejected", "reason", strings.ToLower(string(result.Status)), "country_code", payload.CountryCode, "city", payload.City)
	}
	status := http.StatusOK
	if result.Status == refresh.StatusQueued {
		status = http.StatusAccepted
	}
	writeJSON(writer, status, result)
}

func (s *Server) sourceRefreshByID(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer)
		return
	}
	if s.Refresh == nil {
		writeError(writer, http.StatusNotFound, "refresh not found")
		return
	}
	id := strings.TrimPrefix(request.URL.Path, "/api/v1/source-refresh/")
	jobStatus, ok := s.Refresh.Get(strings.Trim(id, "/"))
	if !ok {
		writeError(writer, http.StatusNotFound, "refresh not found")
		return
	}
	public := map[string]any{"status": jobStatus.Status}
	if jobStatus.RefreshID != "" {
		public["refresh_id"] = jobStatus.RefreshID
	}
	if jobStatus.RetryAfterSeconds > 0 {
		public["retry_after_seconds"] = jobStatus.RetryAfterSeconds
	}
	if len(jobStatus.SourceStatuses) > 0 {
		public["source_statuses"] = jobStatus.SourceStatuses
	}
	public["listings_fetched"] = jobStatus.ListingsFetched
	public["listings_inserted"] = jobStatus.ListingsInserted
	public["listings_updated"] = jobStatus.ListingsUpdated
	public["listings_unchanged"] = jobStatus.ListingsUnchanged
	public["listings_rejected"] = jobStatus.ListingsRejected
	if jobStatus.Error != "" {
		public["error"] = "refresh failed; see server logs"
	}
	writeJSON(writer, http.StatusOK, public)
}

func validateSourceRefresh(payload sourceRefreshRequest) error {
	if payload.CountryCode != "CO" && payload.CountryCode != "UY" {
		return errors.New("country_code must be CO or UY")
	}
	if strings.TrimSpace(payload.City) == "" || len([]rune(payload.City)) > 100 {
		return errors.New("city is required and must be at most 100 characters")
	}
	if len([]rune(payload.Department)) > 100 {
		return errors.New("department must be at most 100 characters")
	}
	if payload.Operation != "" && payload.Operation != "SALE" && payload.Operation != "RENT" {
		return errors.New("operation must be SALE, RENT, or empty for ALL")
	}
	if payload.PropertyType != "" {
		switch payload.PropertyType {
		case "APARTMENT", "HOUSE", "LAND", "COMMERCIAL":
		default:
			return errors.New("property_type is not supported or empty for ALL")
		}
	}
	b := payload.Bounds
	if !b.Valid() || math.IsNaN(b.MinLongitude) || math.IsNaN(b.MaxLongitude) || math.IsNaN(b.MinLatitude) || math.IsNaN(b.MaxLatitude) || math.IsInf(b.MinLongitude, 0) || math.IsInf(b.MaxLongitude, 0) || math.IsInf(b.MinLatitude, 0) || math.IsInf(b.MaxLatitude, 0) {
		return errors.New("bbox coordinates are invalid")
	}
	if b.MinLongitude < -180 || b.MaxLongitude > 180 || b.MinLatitude < -90 || b.MaxLatitude > 90 {
		return errors.New("bbox coordinates are out of range")
	}
	if b.MaxLongitude-b.MinLongitude > 15 || b.MaxLatitude-b.MinLatitude > 15 {
		return errors.New("bbox is too large")
	}
	return nil
}

func (s *Server) health(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), RequestTimeout)
	defer cancel()
	if err := s.Repository.Ping(ctx); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listings(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer)
		return
	}
	filter, err := parseFilter(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), RequestTimeout)
	defer cancel()
	listings, total, err := s.Repository.List(ctx, filter)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "failed to list properties")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"data":       listingResponses(listings),
		"complete":   filter.Offset+len(listings) >= int(total),
		"pagination": map[string]int{"limit": filter.Limit, "offset": filter.Offset, "total": int(total)},
	})
}

func (s *Server) listingByID(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		methodNotAllowed(writer)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/api/v1/listings/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		writeError(writer, http.StatusNotFound, "listing not found")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeError(writer, http.StatusBadRequest, "listing id must be a positive integer")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), RequestTimeout)
	defer cancel()
	if len(parts) == 2 && parts[1] == "refresh-details" {
		if request.Method != http.MethodPost {
			methodNotAllowed(writer)
			return
		}
		if s.Details == nil {
			writeError(writer, http.StatusNotImplemented, "details refresh is not configured")
			return
		}
		listing, err := s.Details.Refresh(ctx, id)
		if errors.Is(err, ErrNotFound) {
			writeError(writer, http.StatusNotFound, "listing not found")
			return
		}
		if errors.Is(err, ErrDetailsUnsupported) {
			writeError(writer, http.StatusNotImplemented, err.Error())
			return
		}
		if errors.Is(err, ErrDetailsRateLimited) {
			writeError(writer, http.StatusTooManyRequests, err.Error())
			return
		}
		if err != nil {
			writeError(writer, http.StatusBadGateway, "failed to refresh listing details")
			return
		}
		writeJSON(writer, http.StatusOK, listingResponse(listing))
		return
	}
	if request.Method != http.MethodGet {
		methodNotAllowed(writer)
		return
	}
	if len(parts) == 2 && parts[1] == "history" {
		if _, err := s.Repository.Get(ctx, id); errors.Is(err, ErrNotFound) {
			writeError(writer, http.StatusNotFound, "listing not found")
			return
		} else if err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to load listing")
			return
		}
		history, err := s.Repository.History(ctx, id)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "failed to load listing history")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"data": history})
		return
	}
	if len(parts) != 1 {
		writeError(writer, http.StatusNotFound, "listing not found")
		return
	}
	listing, err := s.Repository.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		writeError(writer, http.StatusNotFound, "listing not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "failed to load listing")
		return
	}
	writeJSON(writer, http.StatusOK, listingResponse(listing))
}

func (s *Server) stats(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), RequestTimeout)
	defer cancel()
	stats, err := s.Repository.Stats(ctx)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "failed to load statistics")
		return
	}
	writeJSON(writer, http.StatusOK, stats)
}

func parseFilter(request *http.Request) (ListingFilter, error) {
	query := request.URL.Query()
	filter := ListingFilter{
		Source: query.Get("source"), CountryCode: query.Get("country_code"), City: query.Get("city"),
		Operation: query.Get("operation"), PropertyType: query.Get("property_type"),
		Limit: DefaultPageSize,
	}
	var err error
	if value := query.Get("limit"); value != "" {
		filter.Limit, err = strconv.Atoi(value)
		if err != nil || filter.Limit < 1 || filter.Limit > MaxPageSize {
			return ListingFilter{}, fmt.Errorf("limit must be between 1 and %d", MaxPageSize)
		}
	}
	if value := query.Get("offset"); value != "" {
		filter.Offset, err = strconv.Atoi(value)
		if err != nil || filter.Offset < 0 {
			return ListingFilter{}, errors.New("offset must be zero or greater")
		}
	}
	filter.MinPrice, err = optionalInt64(query.Get("min_price"), "min_price")
	if err != nil {
		return ListingFilter{}, err
	}
	filter.MaxPrice, err = optionalInt64(query.Get("max_price"), "max_price")
	if err != nil {
		return ListingFilter{}, err
	}
	filter.MinArea, err = optionalFloat64(query.Get("min_area"), "min_area")
	if err != nil {
		return ListingFilter{}, err
	}
	filter.MaxArea, err = optionalFloat64(query.Get("max_area"), "max_area")
	if err != nil {
		return ListingFilter{}, err
	}
	if filter.MinPrice != nil && filter.MaxPrice != nil && *filter.MinPrice > *filter.MaxPrice {
		return ListingFilter{}, errors.New("min_price must not exceed max_price")
	}
	if filter.MinArea != nil && filter.MaxArea != nil && *filter.MinArea > *filter.MaxArea {
		return ListingFilter{}, errors.New("min_area must not exceed max_area")
	}
	filter.MinLongitude, err = optionalFloat64(query.Get("min_longitude"), "min_longitude")
	if err != nil {
		return ListingFilter{}, err
	}
	filter.MaxLongitude, err = optionalFloat64(query.Get("max_longitude"), "max_longitude")
	if err != nil {
		return ListingFilter{}, err
	}
	filter.MinLatitude, err = optionalFloat64(query.Get("min_latitude"), "min_latitude")
	if err != nil {
		return ListingFilter{}, err
	}
	filter.MaxLatitude, err = optionalFloat64(query.Get("max_latitude"), "max_latitude")
	if err != nil {
		return ListingFilter{}, err
	}
	if filter.MinLongitude != nil && filter.MaxLongitude != nil && *filter.MinLongitude > *filter.MaxLongitude {
		return ListingFilter{}, errors.New("min_longitude must not exceed max_longitude")
	}
	if filter.MinLatitude != nil && filter.MaxLatitude != nil && *filter.MinLatitude > *filter.MaxLatitude {
		return ListingFilter{}, errors.New("min_latitude must not exceed max_latitude")
	}
	return filter, nil
}

func optionalInt64(value, name string) (*int64, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer", name)
	}
	return &parsed, nil
}

func optionalFloat64(value, name string) (*float64, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be a number", name)
	}
	return &parsed, nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

type listingJSON struct {
	ID          int64     `json:"id"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	model.Listing
}

func listingResponse(record ListingRecord) listingJSON {
	return listingJSON{ID: record.ID, FirstSeenAt: record.FirstSeenAt, LastSeenAt: record.LastSeenAt, Listing: record.Listing}
}

func listingResponses(records []ListingRecord) []listingJSON {
	result := make([]listingJSON, 0, len(records))
	for _, record := range records {
		result = append(result, listingResponse(record))
	}
	return result
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

func methodNotAllowed(writer http.ResponseWriter) {
	writer.Header().Set("Allow", http.MethodGet)
	writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
}

func loggingMiddleware(next http.Handler) http.Handler {
	return next
}
