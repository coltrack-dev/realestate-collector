package refresh

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/sources/httpclient"
)

type Status string

const (
	StatusSkipped     Status = "SKIPPED"
	StatusQueued      Status = "QUEUED"
	StatusRunning     Status = "RUNNING"
	StatusCooldown    Status = "COOLDOWN"
	StatusUnsupported Status = "UNSUPPORTED"
	StatusPartial     Status = "PARTIAL"
	StatusDone        Status = "DONE"
	StatusFailed      Status = "FAILED"
)

type Bounds struct {
	MinLongitude float64 `json:"min_longitude"`
	MaxLongitude float64 `json:"max_longitude"`
	MinLatitude  float64 `json:"min_latitude"`
	MaxLatitude  float64 `json:"max_latitude"`
}

func (b Bounds) Valid() bool {
	return !math.IsNaN(b.MinLongitude) && !math.IsNaN(b.MaxLongitude) && !math.IsNaN(b.MinLatitude) && !math.IsNaN(b.MaxLatitude) && !math.IsInf(b.MinLongitude, 0) && !math.IsInf(b.MaxLongitude, 0) && !math.IsInf(b.MinLatitude, 0) && !math.IsInf(b.MaxLatitude, 0) && b.MinLongitude >= -180 && b.MaxLongitude <= 180 && b.MinLatitude >= -90 && b.MaxLatitude <= 90 && b.MinLongitude <= b.MaxLongitude && b.MinLatitude <= b.MaxLatitude
}

type Request struct {
	CountryCode  string
	City         string
	Department   string
	Locality     string
	Bounds       Bounds
	Operation    string
	PropertyType string
}

type CollectResult struct {
	Listings      []model.Listing
	FetchedCount  int
	TotalCount    *int
	PagesFetched  int
	Complete      bool
	PartialReason string
	NextCursor    string
	Skipped       bool
	SkipReason    string
	RejectedCount int
}

type Capabilities struct {
	CountryCode          string
	SupportsBBox         bool
	SupportsCity         bool
	SupportsDepartment   bool
	SupportsLocality     bool
	SupportsOperation    bool
	SupportsPropertyType bool
	// SupportsUnfilteredSearch means the provider accepts an omitted
	// operation and/or property type as an explicit ALL filter.
	SupportsUnfilteredSearch bool
	SupportedMunicipalities  []string
	AllowsAutomatedRefresh   bool
	MinRequestInterval       time.Duration
	CacheTTL                 time.Duration
}

type Source interface {
	Name() string
	Collect(context.Context, Request, int) (CollectResult, error)
}

type Registration struct {
	Name         string
	Capabilities Capabilities
	Factory      func(Request) Source
}

type Saver interface {
	Save(context.Context, model.Listing) (created, updated bool, err error)
}

type Result struct {
	Status            Status `json:"status"`
	RefreshID         string `json:"refresh_id,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

type JobStatus struct {
	Result
	Error             string   `json:"error,omitempty"`
	SourceStatuses    []string `json:"source_statuses,omitempty"`
	ListingsFetched   int      `json:"listings_fetched,omitempty"`
	ListingsInserted  int      `json:"listings_inserted,omitempty"`
	ListingsUpdated   int      `json:"listings_updated,omitempty"`
	ListingsUnchanged int      `json:"listings_unchanged,omitempty"`
	ListingsRejected  int      `json:"listings_rejected,omitempty"`
}

type job struct {
	id        string
	key       requestIdentity
	request   Request
	sources   []Registration
	createdAt time.Time
}

type cacheEntry struct {
	identity  requestIdentity
	expiresAt time.Time
}

type requestIdentity struct {
	Source, CountryCode, City, Department, Locality, Operation, PropertyType string
	MinLongitude, MaxLongitude, MinLatitude, MaxLatitude                     float64
}

type sourceQueryCacheEntry struct {
	identity  requestIdentity
	expiresAt time.Time
}

type Coordinator struct {
	saver         Saver
	queue         chan job
	registrations []Registration

	mu             sync.Mutex
	jobs           map[string]JobStatus
	inFlight       map[requestIdentity]string
	queryInFlight  map[requestIdentity]string
	lastStarted    map[string]time.Time
	cache          []cacheEntry
	queryCache     []sourceQueryCacheEntry
	clientLast     map[string]time.Time
	maxQueue       int
	maxCache       int
	clientInterval time.Duration
	queryTTL       time.Duration
	maxQueryCache  int
	stop           chan struct{}
	logger         *slog.Logger
	enabled        bool
}

func NewCoordinator(saver Saver, registrations []Registration) *Coordinator {
	c := &Coordinator{
		saver: saver, registrations: registrations, queue: make(chan job, 100),
		jobs: make(map[string]JobStatus), inFlight: make(map[requestIdentity]string), queryInFlight: make(map[requestIdentity]string),
		lastStarted: make(map[string]time.Time), clientLast: make(map[string]time.Time),
		maxQueue: 100, maxCache: 128, queryTTL: 15 * time.Minute, maxQueryCache: 128, clientInterval: time.Second, stop: make(chan struct{}), logger: slog.Default(),
	}
	for _, registration := range registrations {
		if registration.Capabilities.AllowsAutomatedRefresh {
			c.enabled = true
			break
		}
	}
	go c.worker()
	return c
}

// SetSourceQueryCache configures the bounded cache for successful provider
// queries. Non-positive values restore the safe defaults.
func (c *Coordinator) SetSourceQueryCache(ttl time.Duration, maxEntries int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	if maxEntries <= 0 {
		maxEntries = 128
	}
	c.queryTTL = ttl
	c.maxQueryCache = maxEntries
	if len(c.queryCache) > c.maxQueryCache {
		c.queryCache = c.queryCache[len(c.queryCache)-c.maxQueryCache:]
	}
}

func (c *Coordinator) Enabled() bool { return c.enabled }

func (c *Coordinator) AvailableSources(countryCode, municipality string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]string, 0)
	for _, registration := range c.registrations {
		if registration.Capabilities.AllowsAutomatedRefresh && strings.EqualFold(registration.Capabilities.CountryCode, countryCode) && supportsMunicipality(registration.Capabilities, municipality) {
			name := registration.Name
			if name != "" {
				result = append(result, name)
			}
		}
	}
	return result
}

func supportsMunicipality(capabilities Capabilities, municipality string) bool {
	if len(capabilities.SupportedMunicipalities) == 0 {
		return true
	}
	for _, supported := range capabilities.SupportedMunicipalities {
		if strings.EqualFold(strings.TrimSpace(supported), strings.TrimSpace(municipality)) {
			return true
		}
	}
	return false
}

func (c *Coordinator) Close() { close(c.stop) }

func (c *Coordinator) Request(request Request, clientKey string) Result {
	return c.request(request, clientKey, true)
}

// RequestBatch submits a bounded set of related geographic queries while
// applying the client throttle once for the batch. Each query still goes
// through the source cache, deduplication and source rate limits.
func (c *Coordinator) RequestBatch(requests []Request, clientKey string) []Result {
	results := make([]Result, len(requests))
	for i, request := range requests {
		results[i] = c.request(request, clientKey, i == 0)
		if i == 0 && results[i].Status == StatusCooldown && results[i].RetryAfterSeconds > 0 {
			for j := i + 1; j < len(requests); j++ {
				results[j] = results[i]
			}
			break
		}
	}
	return results
}

func (c *Coordinator) request(request Request, clientKey string, enforceClientThrottle bool) Result {
	request = normalizeRequest(request)
	c.logger.Info("refresh_requested", "country_code", request.CountryCode, "city", request.City, "operation", request.Operation, "property_type", request.PropertyType)
	if !request.Bounds.Valid() {
		return Result{Status: StatusUnsupported}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(clientKey) == "" {
		clientKey = "anonymous"
	}
	now := time.Now().UTC()
	if enforceClientThrottle {
		if last, ok := c.clientLast[clientKey]; ok && now.Sub(last) < c.clientInterval {
			c.logger.Info("refresh_cooldown", "country_code", request.CountryCode, "city", request.City, "reason", "client_throttle")
			return Result{Status: StatusCooldown, RetryAfterSeconds: int((c.clientInterval - now.Sub(last) + time.Second - 1) / time.Second)}
		}
		c.clientLast[clientKey] = now
	}

	key := requestIdentityFor(request, "")
	if id, ok := c.inFlight[key]; ok {
		c.logger.Debug("refresh_deduplicated", "refresh_id", id, "country_code", request.CountryCode, "city", request.City)
		return Result{Status: StatusQueued, RefreshID: id}
	}
	var selected []Registration
	var joinedRefreshID string
	maxRetry := 0
	eligible := false
	planLimited := false
	for _, registration := range c.registrations {
		capability := registration.Capabilities
		if !capability.AllowsAutomatedRefresh || !strings.EqualFold(capability.CountryCode, request.CountryCode) {
			continue
		}
		if !capability.SupportsBBox && !capability.SupportsCity && !capability.SupportsDepartment && !capability.SupportsLocality {
			continue
		}
		if !supportsMunicipality(capability, request.City) {
			continue
		}
		if capability.SupportsCity && strings.TrimSpace(request.City) == "" && !capability.SupportsLocality && !capability.SupportsDepartment && !capability.SupportsBBox {
			continue
		}
		if (request.Operation == "" || request.PropertyType == "") && !capability.SupportsUnfilteredSearch {
			planLimited = true
			c.logger.Info("refresh_plan_limited", "source", registration.Name, "country_code", request.CountryCode, "city", request.City, "operation", request.Operation, "property_type", request.PropertyType, "reason", "all_filter_not_supported")
			continue
		}
		eligible = true
		name := registration.Name
		if name == "" {
			name = registration.Factory(request).Name()
		}
		last := c.lastStarted[capability.CountryCode+":"+name]
		if capability.MinRequestInterval > 0 && now.Sub(last) < capability.MinRequestInterval {
			remaining := int((capability.MinRequestInterval - now.Sub(last) + time.Second - 1) / time.Second)
			if remaining > maxRetry {
				maxRetry = remaining
			}
			continue
		}
		queryIdentity := sourceQueryIdentityFor(request, name, capability)
		if c.sourceQueryCachedLocked(queryIdentity, now) {
			c.logger.Debug("source_query_cache_hit", "source", name, "country_code", request.CountryCode, "city", request.City, "operation", request.Operation, "property_type", request.PropertyType)
			continue
		}
		if id, ok := c.queryInFlight[queryIdentity]; ok {
			if joinedRefreshID == "" {
				joinedRefreshID = id
			}
			c.logger.Debug("source_query_inflight_deduplicated", "source", name, "refresh_id", id, "country_code", request.CountryCode, "city", request.City)
			continue
		}
		if c.coveredLocked(requestIdentityFor(request, name), now) {
			c.logger.Debug("refresh_cache_hit", "source", name, "country_code", request.CountryCode, "city", request.City)
			continue
		}
		c.logger.Debug("source_query_cache_miss", "source", name, "country_code", request.CountryCode, "city", request.City, "operation", request.Operation, "property_type", request.PropertyType)
		selected = append(selected, registration)
	}
	if len(selected) == 0 {
		if joinedRefreshID != "" {
			return Result{Status: StatusQueued, RefreshID: joinedRefreshID, Reason: "source_query_inflight_deduplicated"}
		}
		if maxRetry > 0 {
			c.logger.Info("refresh_cooldown", "country_code", request.CountryCode, "city", request.City, "retry_after_seconds", maxRetry)
			return Result{Status: StatusCooldown, RetryAfterSeconds: maxRetry}
		}
		if eligible {
			c.logger.Debug("refresh_skipped", "country_code", request.CountryCode, "city", request.City, "reason", "covered_or_no_source")
			return Result{Status: StatusSkipped, Reason: "source_query_cache_hit"}
		}
		if planLimited {
			c.logger.Info("refresh_skipped", "country_code", request.CountryCode, "city", request.City, "operation", request.Operation, "property_type", request.PropertyType, "reason", "refresh_plan_limited")
			return Result{Status: StatusUnsupported}
		}
		return Result{Status: StatusUnsupported}
	}
	c.logger.Info("refresh_plan_created", "country_code", request.CountryCode, "city", request.City, "operation", request.Operation, "property_type", request.PropertyType, "query_count", len(selected))
	if len(c.queue) >= c.maxQueue {
		c.logger.Warn("refresh_cooldown", "country_code", request.CountryCode, "city", request.City, "reason", "queue_full", "queue_length", len(c.queue))
		return Result{Status: StatusCooldown, RetryAfterSeconds: 5}
	}
	id := newID()
	c.jobs[id] = JobStatus{Result: Result{Status: StatusQueued, RefreshID: id}}
	c.inFlight[key] = id
	for _, registration := range selected {
		name := registration.Name
		if name == "" {
			name = registration.Factory(request).Name()
		}
		c.queryInFlight[sourceQueryIdentityFor(request, name, registration.Capabilities)] = id
	}
	c.queue <- job{id: id, key: key, request: request, sources: selected, createdAt: now}
	c.logger.Info("refresh_queued", "refresh_id", id, "country_code", request.CountryCode, "city", request.City, "queue_length", len(c.queue))
	return Result{Status: StatusQueued, RefreshID: id}
}

func (c *Coordinator) Get(id string) (JobStatus, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	status, ok := c.jobs[id]
	return status, ok
}

func (c *Coordinator) worker() {
	for {
		select {
		case <-c.stop:
			return
		case current := <-c.queue:
			c.run(current)
		}
	}
}

func (c *Coordinator) run(current job) {
	started := time.Now()
	waitMS := started.Sub(current.createdAt).Milliseconds()
	c.logger.Info("refresh_started", "refresh_id", current.id, "queue_wait_duration_ms", waitMS, "queue_length", len(c.queue), "country_code", current.request.CountryCode, "city", current.request.City, "operation", current.request.Operation, "property_type", current.request.PropertyType)
	c.mu.Lock()
	c.jobs[current.id] = JobStatus{Result: Result{Status: StatusRunning, RefreshID: current.id}}
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var runErr error
	listingsFetched, listingsInserted, listingsUpdated, listingsUnchanged, listingsRejected, snapshots := 0, 0, 0, 0, 0, 0
	partial := false
	partialReasons := make([]string, 0)
	failedSources, successfulSources, skippedSources := 0, 0, 0
	sourceStatuses := make([]string, 0, len(current.sources))
	for _, registration := range current.sources {
		name := registration.Name
		if name == "" {
			name = registration.Factory(current.request).Name()
		}
		c.mu.Lock()
		c.lastStarted[registration.Capabilities.CountryCode+":"+name] = time.Now().UTC()
		c.mu.Unlock()
		source := registration.Factory(current.request)
		sourceContext := httpclient.WithMetadata(ctx, httpclient.RequestMetadata{Source: name, RefreshID: current.id, Operation: current.request.Operation, City: current.request.City, CountryCode: current.request.CountryCode})
		requestStarted := time.Now()
		c.logger.Info("source_request_started", "refresh_id", current.id, "source", name, "country_code", current.request.CountryCode, "city", current.request.City, "operation", current.request.Operation, "property_type", current.request.PropertyType)
		collected, err := source.Collect(sourceContext, current.request, 50)
		if err != nil {
			failedSources++
			sourceStatuses = append(sourceStatuses, name+":failed")
			c.logger.Warn("refresh_source_error", "refresh_id", current.id, "source", name, "error", err)
			c.logger.Warn("source_request_completed", "refresh_id", current.id, "source", name, "result", "error", "duration_ms", time.Since(requestStarted).Milliseconds(), "reason", "adapter_error")
			runErr = errors.Join(runErr, fmt.Errorf("%s: %w", name, err))
			partialReasons = append(partialReasons, name+": request_failed")
			continue
		}
		c.logger.Info("source_request_completed", "refresh_id", current.id, "source", name, "result", "success", "duration_ms", time.Since(requestStarted).Milliseconds(), "fetched", collected.FetchedCount, "rejected", collected.RejectedCount, "complete", collected.Complete, "reason", collected.PartialReason)
		if collected.Skipped {
			skippedSources++
			sourceStatuses = append(sourceStatuses, name+":skipped:"+collected.SkipReason)
			partial = true
			if collected.SkipReason != "" {
				partialReasons = append(partialReasons, name+": "+collected.SkipReason)
			}
			c.logger.Info("refresh_source_skipped", "refresh_id", current.id, "source", name, "reason", collected.SkipReason)
			continue
		}
		successfulSources++
		sourceStatuses = append(sourceStatuses, name+":success")
		listingsFetched += collected.FetchedCount
		listingsRejected += collected.RejectedCount
		c.logger.Info("source_items_parsed", "refresh_id", current.id, "source", name, "parsed", collected.FetchedCount, "rejected", collected.RejectedCount, "pages_fetched", collected.PagesFetched, "complete", collected.Complete, "reason", collected.PartialReason)
		if collected.RejectedCount > 0 {
			c.logger.Warn("source_items_rejected", "refresh_id", current.id, "source", name, "rejected", collected.RejectedCount, "reason", "normalization_or_missing_identifier")
		}
		if !collected.Complete || !registration.Capabilities.SupportsBBox {
			partial = true
			if !registration.Capabilities.SupportsBBox {
				partialReasons = append(partialReasons, name+": city_filter_does_not_prove_bbox_coverage")
			} else if collected.PartialReason != "" {
				partialReasons = append(partialReasons, name+": "+collected.PartialReason)
			}
		}
		sourceSaveFailed := false
		for _, listing := range collected.Listings {
			created, updated, err := c.saver.Save(ctx, listing)
			if err != nil {
				sourceSaveFailed = true
				runErr = errors.Join(runErr, err)
				c.logger.Warn("refresh_save_error", "refresh_id", current.id, "source", name, "reason", "postgres_save", "error", err)
			} else {
				if created {
					listingsInserted++
					c.logger.Debug("listings_inserted", "refresh_id", current.id, "source", name, "count", 1)
				}
				if updated {
					listingsUpdated++
					snapshots++
					c.logger.Debug("listings_updated", "refresh_id", current.id, "source", name, "count", 1)
				}
				if !created && !updated {
					listingsUnchanged++
					c.logger.Debug("listings_unchanged", "refresh_id", current.id, "source", name, "count", 1)
				}
			}
		}
		if collected.Complete && registration.Capabilities.SupportsBBox && !sourceSaveFailed {
			c.mu.Lock()
			ttl := registration.Capabilities.CacheTTL
			if ttl <= 0 {
				ttl = 5 * time.Minute
			}
			c.cache = append(c.cache, cacheEntry{identity: requestIdentityFor(current.request, name), expiresAt: time.Now().UTC().Add(ttl)})
			if len(c.cache) > c.maxCache {
				c.cache = c.cache[len(c.cache)-c.maxCache:]
			}
			c.mu.Unlock()
		}
		if !sourceSaveFailed {
			c.mu.Lock()
			c.storeSourceQueryCacheLocked(sourceQueryIdentityFor(current.request, name, registration.Capabilities), time.Now().UTC())
			c.mu.Unlock()
		}
	}
	c.mu.Lock()
	delete(c.inFlight, current.key)
	for _, registration := range current.sources {
		name := registration.Name
		if name == "" {
			name = registration.Factory(current.request).Name()
		}
		delete(c.queryInFlight, sourceQueryIdentityFor(current.request, name, registration.Capabilities))
	}
	if runErr != nil && successfulSources == 0 {
		c.jobs[current.id] = JobStatus{Result: Result{Status: StatusFailed, RefreshID: current.id}, Error: runErr.Error(), SourceStatuses: sourceStatuses, ListingsFetched: listingsFetched, ListingsInserted: listingsInserted, ListingsUpdated: listingsUpdated, ListingsUnchanged: listingsUnchanged, ListingsRejected: listingsRejected}
		c.logger.Error("refresh_failed", "refresh_id", current.id, "execution_duration_ms", time.Since(started).Milliseconds(), "listings_fetched", listingsFetched, "listings_inserted", listingsInserted, "listings_updated", listingsUpdated, "snapshot_count", snapshots, "error", runErr)
	} else if runErr != nil || partial || skippedSources > 0 || failedSources > 0 {
		c.jobs[current.id] = JobStatus{Result: Result{Status: StatusPartial, RefreshID: current.id}, Error: strings.Join(partialReasons, "; "), SourceStatuses: sourceStatuses, ListingsFetched: listingsFetched, ListingsInserted: listingsInserted, ListingsUpdated: listingsUpdated, ListingsUnchanged: listingsUnchanged, ListingsRejected: listingsRejected}
		c.logger.Info("refresh_completed", "refresh_id", current.id, "status", StatusPartial, "execution_duration_ms", time.Since(started).Milliseconds(), "listings_fetched", listingsFetched, "listings_inserted", listingsInserted, "listings_updated", listingsUpdated, "snapshot_count", snapshots, "source_statuses", sourceStatuses, "reason", strings.Join(partialReasons, "; "))
	} else {
		c.jobs[current.id] = JobStatus{Result: Result{Status: StatusDone, RefreshID: current.id}, SourceStatuses: sourceStatuses, ListingsFetched: listingsFetched, ListingsInserted: listingsInserted, ListingsUpdated: listingsUpdated, ListingsUnchanged: listingsUnchanged, ListingsRejected: listingsRejected}
		c.logger.Info("refresh_completed", "refresh_id", current.id, "execution_duration_ms", time.Since(started).Milliseconds(), "listings_fetched", listingsFetched, "listings_inserted", listingsInserted, "listings_updated", listingsUpdated, "snapshot_count", snapshots)
	}
	c.mu.Unlock()
}

func (c *Coordinator) coveredLocked(identity requestIdentity, now time.Time) bool {
	for _, entry := range c.cache {
		if entry.identity == identity && entry.expiresAt.After(now) {
			return true
		}
	}
	return false
}

func sourceQueryIdentityFor(request Request, source string, capabilities Capabilities) requestIdentity {
	identity := requestIdentityFor(request, source)
	if !capabilities.SupportsBBox {
		identity.MinLongitude = 0
		identity.MaxLongitude = 0
		identity.MinLatitude = 0
		identity.MaxLatitude = 0
	}
	if !capabilities.SupportsDepartment {
		identity.Department = ""
	}
	if !capabilities.SupportsCity {
		identity.City = ""
	}
	if !capabilities.SupportsLocality {
		identity.Locality = ""
	}
	if !capabilities.SupportsOperation {
		identity.Operation = ""
	}
	if !capabilities.SupportsPropertyType {
		identity.PropertyType = ""
	}
	return identity
}

func (c *Coordinator) sourceQueryCachedLocked(identity requestIdentity, now time.Time) bool {
	valid := c.queryCache[:0]
	hit := false
	for _, entry := range c.queryCache {
		if !entry.expiresAt.After(now) {
			continue
		}
		valid = append(valid, entry)
		if entry.identity == identity {
			hit = true
		}
	}
	c.queryCache = valid
	return hit
}

func (c *Coordinator) storeSourceQueryCacheLocked(identity requestIdentity, now time.Time) {
	for _, entry := range c.queryCache {
		if entry.identity == identity {
			return
		}
	}
	c.queryCache = append(c.queryCache, sourceQueryCacheEntry{identity: identity, expiresAt: now.Add(c.queryTTL)})
	if len(c.queryCache) > c.maxQueryCache {
		c.queryCache = c.queryCache[len(c.queryCache)-c.maxQueryCache:]
	}
	c.logger.Debug("source_query_cache_store", "source", identity.Source, "country_code", identity.CountryCode, "city", identity.City, "operation", identity.Operation, "property_type", identity.PropertyType, "ttl", c.queryTTL.String())
}

func normalizeRequest(request Request) Request {
	request.CountryCode = strings.ToUpper(strings.TrimSpace(request.CountryCode))
	request.City = strings.TrimSpace(request.City)
	request.Department = strings.TrimSpace(request.Department)
	request.Locality = strings.TrimSpace(request.Locality)
	request.Operation = strings.ToUpper(strings.TrimSpace(request.Operation))
	request.PropertyType = strings.ToUpper(strings.TrimSpace(request.PropertyType))
	return request
}

func requestIdentityFor(request Request, source string) requestIdentity {
	request = normalizeRequest(request)
	return requestIdentity{Source: strings.ToLower(strings.TrimSpace(source)), CountryCode: request.CountryCode, City: strings.ToLower(request.City), Department: strings.ToLower(request.Department), Locality: strings.ToLower(request.Locality), Operation: request.Operation, PropertyType: request.PropertyType, MinLongitude: request.Bounds.MinLongitude, MaxLongitude: request.Bounds.MaxLongitude, MinLatitude: request.Bounds.MinLatitude, MaxLatitude: request.Bounds.MaxLatitude}
}
func newID() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("refresh-%d", time.Now().UnixNano())
	}
	return "refresh-" + hex.EncodeToString(bytes[:])
}
