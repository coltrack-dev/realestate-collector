package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/example/realestate-collector/internal/model"
)

var ErrDetailsUnsupported = errors.New("details refresh is unsupported for this source")
var ErrDetailsRateLimited = errors.New("details refresh was requested too recently")

type DetailSource interface {
	Name() string
	FetchDetails(context.Context, string) (model.Listing, error)
}

type DetailRepository interface {
	Get(context.Context, int64) (ListingRecord, error)
	SetDetailsStatus(context.Context, int64, string) error
	UpdateDetails(context.Context, int64, model.Listing, time.Time, time.Time) error
}

type DetailsService struct {
	Repository DetailRepository
	Sources    map[string]DetailSource
	TTL        time.Duration

	mu               sync.Mutex
	calls            map[int64]*detailsCall
	attempts         map[int64]time.Time
	MinRetryInterval time.Duration
}

type detailsCall struct {
	done   chan struct{}
	record ListingRecord
	err    error
}

func NewDetailsService(repository DetailRepository, sources []DetailSource, ttl time.Duration) *DetailsService {
	byName := make(map[string]DetailSource, len(sources))
	for _, source := range sources {
		byName[source.Name()] = source
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &DetailsService{Repository: repository, Sources: byName, TTL: ttl, calls: make(map[int64]*detailsCall), attempts: make(map[int64]time.Time), MinRetryInterval: time.Minute}
}

func (s *DetailsService) Refresh(ctx context.Context, listingID int64) (ListingRecord, error) {
	record, err := s.Repository.Get(ctx, listingID)
	if err != nil {
		return ListingRecord{}, err
	}
	if record.Listing.DetailsStatus == "READY" && record.Listing.DetailsExpiresAt != nil && record.Listing.DetailsExpiresAt.After(time.Now().UTC()) {
		return record, nil
	}
	source, ok := s.Sources[record.Listing.Source]
	if !ok {
		_ = s.Repository.SetDetailsStatus(ctx, listingID, "UNSUPPORTED")
		record.Listing.DetailsStatus = "UNSUPPORTED"
		return record, ErrDetailsUnsupported
	}

	s.mu.Lock()
	if call, exists := s.calls[listingID]; exists {
		s.mu.Unlock()
		select {
		case <-call.done:
			return call.record, call.err
		case <-ctx.Done():
			return ListingRecord{}, ctx.Err()
		}
	}
	if lastAttempt, exists := s.attempts[listingID]; exists && time.Since(lastAttempt) < s.MinRetryInterval {
		s.mu.Unlock()
		return record, ErrDetailsRateLimited
	}
	s.attempts[listingID] = time.Now()
	call := &detailsCall{done: make(chan struct{})}
	s.calls[listingID] = call
	s.mu.Unlock()

	call.record, call.err = s.refresh(ctx, record, source)
	s.mu.Lock()
	delete(s.calls, listingID)
	close(call.done)
	s.mu.Unlock()
	return call.record, call.err
}

func (s *DetailsService) refresh(ctx context.Context, record ListingRecord, source DetailSource) (ListingRecord, error) {
	if err := s.Repository.SetDetailsStatus(ctx, record.ID, "LOADING"); err != nil {
		return record, err
	}
	details, err := source.FetchDetails(ctx, record.Listing.ExternalID)
	if err != nil {
		_ = s.Repository.SetDetailsStatus(ctx, record.ID, "FAILED")
		return record, fmt.Errorf("fetch %s details: %w", source.Name(), err)
	}
	now := time.Now().UTC()
	if err := s.Repository.UpdateDetails(ctx, record.ID, details, now, now.Add(s.TTL)); err != nil {
		_ = s.Repository.SetDetailsStatus(ctx, record.ID, "FAILED")
		return record, err
	}
	return s.Repository.Get(ctx, record.ID)
}
