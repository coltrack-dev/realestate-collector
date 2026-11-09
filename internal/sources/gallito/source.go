package gallito

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/example/realestate-collector/internal/collector"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/normalize"
	"github.com/example/realestate-collector/internal/sources/httpclient"
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
	ProgressHook  func(*Progress) error

	limiter *rateLimiter
}

type SearchJob struct {
	Region       string
	City         string
	Department   string
	Operation    string
	PropertyType string
}

type JobProgress struct {
	SearchID  string `json:"search_id"`
	Page      int    `json:"page"`
	Completed bool   `json:"completed"`
}

type Progress struct {
	Signature string                 `json:"signature"`
	Jobs      map[string]JobProgress `json:"jobs"`
	SeenIDs   map[string]bool        `json:"seen_ids"`
}

// ProgressSignature identifies the search configuration that produced a
// progress file. A cursor from another configuration must never be resumed.
func ProgressSignature(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(append([]string{"gallito-progress-v1"}, parts...), "\x00")))
	return fmt.Sprintf("%x", digest[:])
}

type JobStats struct {
	Job               SearchJob
	SearchID          string
	PagesFetched      int
	ListingsFound     int
	ListingsAccepted  int
	Duplicates        int
	LastPage          int
	ActualCities      []string
	TerminationReason string
}

type rateLimiter struct {
	mu          sync.Mutex
	lastRequest time.Time
}

func New(httpClient *http.Client, userAgent string, delay time.Duration) Source {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	httpClient.Transport = httpclient.NewLoggingTransport(httpClient.Transport, nil, "gallito")
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
		s.HTTP.Transport = httpclient.NewLoggingTransport(s.HTTP.Transport, nil, "gallito")
	}
	jobs := []SearchJob{{City: s.City, Operation: s.Operation}}
	if len(s.PropertyTypes) > 0 {
		jobs[0].PropertyType = s.PropertyTypes[0]
	}
	listings, _, err := s.CollectJobs(ctx, jobs, limit, 10, nil)
	return listings, err
}

// CollectJobs runs separate public searches and deduplicates listing IDs across
// them. It never calls get_listing; details remain lazy.
func (s Source) CollectJobs(ctx context.Context, jobs []SearchJob, limit, maxPages int, progress *Progress) ([]model.Listing, []JobStats, error) {
	if limit <= 0 {
		return nil, nil, fmt.Errorf("limit must be greater than zero")
	}
	if maxPages <= 0 || maxPages > 10 {
		maxPages = 10
	}
	if progress == nil {
		progress = &Progress{}
	}
	if progress.Jobs == nil {
		progress.Jobs = make(map[string]JobProgress)
	}
	if progress.SeenIDs == nil {
		progress.SeenIDs = make(map[string]bool)
	}
	listings := make([]model.Listing, 0, min(limit, 100))
	stats := make([]JobStats, 0, len(jobs))
	jobStats := make(map[string]*JobStats, len(jobs))
	for _, job := range jobs {
		key := jobKey(job)
		jobStats[key] = &JobStats{Job: job}
	}

	for {
		activeJobs := 0
		for _, job := range jobs {
			key := jobKey(job)
			jobStat := jobStats[key]
			state := progress.Jobs[key]
			if state.Completed {
				continue
			}
			if len(listings) >= limit {
				if jobStat.TerminationReason == "" {
					jobStat.TerminationReason = "global_limit_reached_before_job"
				}
				state.Completed = true
				progress.Jobs[key] = state
				continue
			}
			if state.Page >= maxPages {
				jobStat.TerminationReason = "job_max_pages_reached"
				state.Completed = true
				progress.Jobs[key] = state
				continue
			}
			activeJobs++

			page, nextState, err := s.fetchJobPage(ctx, job, state)
			if err != nil {
				return listings, flattenJobStats(jobs, jobStats), err
			}
			jobStat.SearchID = page.SearchID
			pageNo := pageNumber(page, state)
			jobStat.PagesFetched++
			jobStat.LastPage = pageNo
			jobStat.ListingsFound += len(page.Listings)
			for _, item := range page.Listings {
				if item.City != "" && !containsString(jobStat.ActualCities, item.City) {
					jobStat.ActualCities = append(jobStat.ActualCities, item.City)
				}
				if progress.SeenIDs[item.ID] {
					jobStat.Duplicates++
					continue
				}
				listing, listingErr := toListing(item)
				if listingErr != nil {
					continue
				}
				progress.SeenIDs[item.ID] = true
				listings = append(listings, listing)
				jobStat.ListingsAccepted++
				if len(listings) >= limit {
					break
				}
			}

			nextState.SearchID = page.SearchID
			nextState.Page = pageNo
			nextState.Completed = page.SearchID == "" || pageNo >= page.TotalPages || pageNo >= maxPages
			if nextState.Completed && jobStat.TerminationReason == "" {
				switch {
				case len(listings) >= limit:
					jobStat.TerminationReason = "global_limit_reached"
				case page.SearchID == "":
					jobStat.TerminationReason = "empty_search_id"
				case pageNo >= maxPages:
					jobStat.TerminationReason = "job_max_pages_reached"
				default:
					jobStat.TerminationReason = "search_exhausted"
				}
			}
			progress.Jobs[key] = nextState
			if s.ProgressHook != nil {
				if err := s.ProgressHook(progress); err != nil {
					return listings, flattenJobStats(jobs, jobStats), err
				}
			}
		}
		if len(listings) >= limit {
			for _, job := range jobs {
				jobStat := jobStats[jobKey(job)]
				if jobStat.TerminationReason == "" {
					jobStat.TerminationReason = "global_limit_reached_before_job"
				}
			}
			break
		}
		if activeJobs == 0 {
			break
		}
	}
	stats = flattenJobStats(jobs, jobStats)
	return listings, stats, nil
}

func (s Source) fetchJobPage(ctx context.Context, job SearchJob, state JobProgress) (searchResponse, JobProgress, error) {
	if state.SearchID != "" && state.Page > 0 {
		var page searchResponse
		err := s.call(ctx, "more_results", moreResultsArguments{SearchID: state.SearchID, Page: state.Page + 1}, &page)
		if err == nil {
			return page, state, nil
		}
		if !isExpiredSearchError(err) {
			return searchResponse{}, state, fmt.Errorf("resume Gallito job %s: %w", jobKey(job), err)
		}
		state = JobProgress{}
	}
	arguments := searchArguments{Operation: job.Operation, City: job.City, Department: job.Department,
		PropertyTypes: nonEmpty(job.PropertyType), IncludeProjects: false, IncludeListings: true, Sort: "newest", Language: "es"}
	var page searchResponse
	if err := s.call(ctx, "search_listings", arguments, &page); err != nil {
		return searchResponse{}, state, fmt.Errorf("start Gallito job %s: %w", jobKey(job), err)
	}
	return page, state, nil
}

func isExpiredSearchError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "searchid") && (strings.Contains(message, "expired") || strings.Contains(message, "not found") || strings.Contains(message, "unknown"))
}

func flattenJobStats(jobs []SearchJob, values map[string]*JobStats) []JobStats {
	result := make([]JobStats, 0, len(jobs))
	for _, job := range jobs {
		if value := values[jobKey(job)]; value != nil {
			result = append(result, *value)
		}
	}
	return result
}

func pageNumber(page searchResponse, previous JobProgress) int {
	if page.Page > 0 {
		return page.Page
	}
	if previous.Page > 0 {
		return previous.Page + 1
	}
	return 1
}

func jobKey(job SearchJob) string {
	return strings.Join([]string{job.Region, job.City, job.Department, job.Operation, job.PropertyType}, "|")
}
func nonEmpty(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func LoadProgress(path string) (*Progress, error) {
	if path == "" {
		return &Progress{Jobs: map[string]JobProgress{}, SeenIDs: map[string]bool{}}, nil
	}
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return &Progress{Jobs: map[string]JobProgress{}, SeenIDs: map[string]bool{}}, nil
		}
		return nil, fmt.Errorf("read Gallito progress: %w", err)
	}
	var progress Progress
	if err := json.Unmarshal(body, &progress); err != nil {
		return nil, fmt.Errorf("decode Gallito progress: %w", err)
	}
	return &progress, nil
}

func SaveProgress(path string, progress *Progress) error {
	if path == "" || progress == nil {
		return nil
	}
	body, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Gallito progress: %w", err)
	}
	if err := os.WriteFile(filepath.Clean(path), append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("write Gallito progress: %w", err)
	}
	return nil
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
			return fmt.Errorf("Gallito MCP %s: HTTP %d", method, response.StatusCode)
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
