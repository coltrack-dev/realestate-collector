package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/realestate-collector/internal/model"
)

const (
	DefaultPageSize = 50
	MaxPageSize     = 100
	RequestTimeout  = 5 * time.Second
)

var ErrNotFound = errors.New("listing not found")

type ListingFilter struct {
	Source, City, Operation, PropertyType string
	MinPrice, MaxPrice                     *int64
	MinArea, MaxArea                       *float64
	Limit, Offset                          int
}

type Snapshot struct {
	ID          int64 `json:"id"`
	Price       *int64 `json:"price"`
	Currency    *string `json:"currency"`
	AreaM2      *float64 `json:"area_m2"`
	ContentHash string `json:"content_hash"`
	CollectedAt time.Time `json:"collected_at"`
}

type ListingRecord struct {
	ID          int64
	Listing     model.Listing
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

type Stats struct {
	Total           int64            `json:"total"`
	BySource        map[string]int64 `json:"by_source"`
	ByOperation     map[string]int64 `json:"by_operation"`
	LastUpdatedAt   *time.Time       `json:"last_updated_at"`
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
}

func NewServer(repository Repository) http.Handler {
	server := &Server{Repository: repository}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.health)
	mux.HandleFunc("/api/v1/listings", server.listings)
	mux.HandleFunc("/api/v1/listings/", server.listingByID)
	mux.HandleFunc("/api/v1/stats", server.stats)
	return loggingMiddleware(mux)
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
		"data": listingResponses(listings),
		"pagination": map[string]int{"limit": filter.Limit, "offset": filter.Offset, "total": int(total)},
	})
}

func (s *Server) listingByID(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
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
		Source: query.Get("source"), City: query.Get("city"),
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
	if err != nil { return ListingFilter{}, err }
	filter.MaxPrice, err = optionalInt64(query.Get("max_price"), "max_price")
	if err != nil { return ListingFilter{}, err }
	filter.MinArea, err = optionalFloat64(query.Get("min_area"), "min_area")
	if err != nil { return ListingFilter{}, err }
	filter.MaxArea, err = optionalFloat64(query.Get("max_area"), "max_area")
	if err != nil { return ListingFilter{}, err }
	if filter.MinPrice != nil && filter.MaxPrice != nil && *filter.MinPrice > *filter.MaxPrice { return ListingFilter{}, errors.New("min_price must not exceed max_price") }
	if filter.MinArea != nil && filter.MaxArea != nil && *filter.MinArea > *filter.MaxArea { return ListingFilter{}, errors.New("min_area must not exceed max_area") }
	return filter, nil
}

func optionalInt64(value, name string) (*int64, error) {
	if value == "" { return nil, nil }
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil { return nil, fmt.Errorf("%s must be an integer", name) }
	return &parsed, nil
}

func optionalFloat64(value, name string) (*float64, error) {
	if value == "" { return nil, nil }
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil { return nil, fmt.Errorf("%s must be a number", name) }
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
	for _, record := range records { result = append(result, listingResponse(record)) }
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
