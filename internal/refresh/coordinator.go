package refresh

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/example/realestate-collector/internal/model"
)

type Status string

const (
	StatusSkipped     Status = "SKIPPED"
	StatusQueued      Status = "QUEUED"
	StatusRunning     Status = "RUNNING"
	StatusCooldown    Status = "COOLDOWN"
	StatusUnsupported Status = "UNSUPPORTED"
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
	return b.MinLongitude <= b.MaxLongitude && b.MinLatitude <= b.MaxLatitude
}

type Request struct {
	CountryCode  string
	City         string
	Bounds       Bounds
	Operation    string
	PropertyType string
}

type Capabilities struct {
	CountryCode            string
	SupportsBBox           bool
	SupportsCity           bool
	SupportsDepartment     bool
	SupportsOperation      bool
	SupportsPropertyType   bool
	AllowsAutomatedRefresh bool
	MinRequestInterval     time.Duration
	CacheTTL               time.Duration
}

type Source interface {
	Name() string
	Collect(context.Context, Request, int) ([]model.Listing, error)
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
}

type JobStatus struct {
	Result
	Error string `json:"error,omitempty"`
}

type job struct {
	id        string
	key       string
	request   Request
	sources   []Registration
	createdAt time.Time
}

type cacheEntry struct {
	source    string
	bounds    Bounds
	expiresAt time.Time
}

type Coordinator struct {
	saver         Saver
	queue         chan job
	registrations []Registration

	mu             sync.Mutex
	jobs           map[string]JobStatus
	inFlight       map[string]string
	lastStarted    map[string]time.Time
	cache          []cacheEntry
	clientLast     map[string]time.Time
	maxQueue       int
	maxCache       int
	clientInterval time.Duration
	stop           chan struct{}
}

func NewCoordinator(saver Saver, registrations []Registration) *Coordinator {
	c := &Coordinator{
		saver: saver, registrations: registrations, queue: make(chan job, 100),
		jobs: make(map[string]JobStatus), inFlight: make(map[string]string),
		lastStarted: make(map[string]time.Time), clientLast: make(map[string]time.Time),
		maxQueue: 100, maxCache: 128, clientInterval: time.Second, stop: make(chan struct{}),
	}
	go c.worker()
	return c
}

func (c *Coordinator) Close() { close(c.stop) }

func (c *Coordinator) Request(request Request, clientKey string) Result {
	if !request.Bounds.Valid() {
		return Result{Status: StatusUnsupported}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(clientKey) == "" {
		clientKey = "anonymous"
	}
	now := time.Now().UTC()
	if last, ok := c.clientLast[clientKey]; ok && now.Sub(last) < c.clientInterval {
		return Result{Status: StatusCooldown, RetryAfterSeconds: int((c.clientInterval - now.Sub(last) + time.Second - 1) / time.Second)}
	}
	c.clientLast[clientKey] = now

	key := requestKey(request)
	if id, ok := c.inFlight[key]; ok {
		return Result{Status: StatusQueued, RefreshID: id}
	}
	var selected []Registration
	maxRetry := 0
	eligible := false
	for _, registration := range c.registrations {
		capability := registration.Capabilities
		if !capability.AllowsAutomatedRefresh || !strings.EqualFold(capability.CountryCode, request.CountryCode) {
			continue
		}
		if !capability.SupportsBBox && !capability.SupportsCity {
			continue
		}
		if capability.SupportsCity && strings.TrimSpace(request.City) == "" && !capability.SupportsBBox {
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
		if c.coveredLocked(name, request.Bounds, now) {
			continue
		}
		selected = append(selected, registration)
	}
	if len(selected) == 0 {
		if maxRetry > 0 {
			return Result{Status: StatusCooldown, RetryAfterSeconds: maxRetry}
		}
		if eligible {
			return Result{Status: StatusSkipped}
		}
		return Result{Status: StatusUnsupported}
	}
	if len(c.queue) >= c.maxQueue {
		return Result{Status: StatusCooldown, RetryAfterSeconds: 5}
	}
	id := newID()
	c.jobs[id] = JobStatus{Result: Result{Status: StatusQueued, RefreshID: id}}
	c.inFlight[key] = id
	c.queue <- job{id: id, key: key, request: request, sources: selected, createdAt: now}
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
	c.mu.Lock()
	c.jobs[current.id] = JobStatus{Result: Result{Status: StatusRunning, RefreshID: current.id}}
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var runErr error
	for _, registration := range current.sources {
		name := registration.Name
		if name == "" {
			name = registration.Factory(current.request).Name()
		}
		c.mu.Lock()
		c.lastStarted[registration.Capabilities.CountryCode+":"+name] = time.Now().UTC()
		c.mu.Unlock()
		source := registration.Factory(current.request)
		listings, err := source.Collect(ctx, current.request, 50)
		if err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("%s: %w", name, err))
			continue
		}
		for _, listing := range listings {
			if _, _, err := c.saver.Save(ctx, listing); err != nil {
				runErr = errors.Join(runErr, err)
			}
		}
		c.mu.Lock()
		ttl := registration.Capabilities.CacheTTL
		if ttl <= 0 {
			ttl = 5 * time.Minute
		}
		c.cache = append(c.cache, cacheEntry{source: name, bounds: current.request.Bounds, expiresAt: time.Now().UTC().Add(ttl)})
		if len(c.cache) > c.maxCache {
			c.cache = c.cache[len(c.cache)-c.maxCache:]
		}
		c.mu.Unlock()
	}
	c.mu.Lock()
	delete(c.inFlight, current.key)
	if runErr != nil {
		c.jobs[current.id] = JobStatus{Result: Result{Status: StatusFailed, RefreshID: current.id}, Error: runErr.Error()}
	} else {
		c.jobs[current.id] = JobStatus{Result: Result{Status: StatusDone, RefreshID: current.id}}
	}
	c.mu.Unlock()
}

func (c *Coordinator) coveredLocked(source string, bounds Bounds, now time.Time) bool {
	for _, entry := range c.cache {
		if entry.source == source && entry.expiresAt.After(now) && entry.bounds.MinLongitude <= bounds.MinLongitude && entry.bounds.MaxLongitude >= bounds.MaxLongitude && entry.bounds.MinLatitude <= bounds.MinLatitude && entry.bounds.MaxLatitude >= bounds.MaxLatitude {
			return true
		}
	}
	return false
}

func requestKey(request Request) string {
	return fmt.Sprintf("%s|%s|%.2f,%.2f,%.2f,%.2f|%s|%s", strings.ToUpper(request.CountryCode), strings.ToLower(request.City), request.Bounds.MinLongitude, request.Bounds.MaxLongitude, request.Bounds.MinLatitude, request.Bounds.MaxLatitude, strings.ToUpper(request.Operation), strings.ToUpper(request.PropertyType))
}
func newID() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("refresh-%d", time.Now().UnixNano())
	}
	return "refresh-" + hex.EncodeToString(bytes[:])
}
