package api

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/example/realestate-collector/internal/model"
)

type detailsRepository struct {
	mu       sync.Mutex
	record   ListingRecord
	statuses []string
	updates  int
}

func (r *detailsRepository) Get(context.Context, int64) (ListingRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.record.ID == 0 {
		return ListingRecord{}, ErrNotFound
	}
	return r.record, nil
}
func (r *detailsRepository) SetDetailsStatus(_ context.Context, _ int64, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, status)
	r.record.Listing.DetailsStatus = status
	return nil
}
func (r *detailsRepository) UpdateDetails(_ context.Context, _ int64, listing model.Listing, fetchedAt, expiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates++
	r.record.Listing.Description = listing.Description
	r.record.Listing.Images = listing.Images
	r.record.Listing.DetailsFetchedAt = &fetchedAt
	r.record.Listing.DetailsExpiresAt = &expiresAt
	r.record.Listing.DetailsStatus = "READY"
	return nil
}

type detailsSource struct {
	mu    sync.Mutex
	calls int
	fn    func() (model.Listing, error)
}

func (s *detailsSource) Name() string { return "vixora" }
func (s *detailsSource) FetchDetails(context.Context, string) (model.Listing, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.fn()
}

func TestDetailsServiceUsesTTLCache(t *testing.T) {
	repository := &detailsRepository{record: ListingRecord{ID: 1, Listing: model.Listing{Source: "vixora", ExternalID: "A", DetailsStatus: "NOT_LOADED"}}}
	source := &detailsSource{fn: func() (model.Listing, error) { return model.Listing{Description: "full", Images: []string{"a"}}, nil }}
	service := NewDetailsService(repository, []DetailSource{source}, time.Hour)
	if _, err := service.Refresh(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.calls != 1 || repository.updates != 1 {
		t.Fatalf("calls=%d updates=%d", source.calls, repository.updates)
	}
}

func TestDetailsServiceDeduplicatesConcurrentRequests(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	repository := &detailsRepository{record: ListingRecord{ID: 2, Listing: model.Listing{Source: "vixora", ExternalID: "B", DetailsStatus: "NOT_LOADED"}}}
	source := &detailsSource{fn: func() (model.Listing, error) {
		close(started)
		<-release
		return model.Listing{Description: "full"}, nil
	}}
	service := NewDetailsService(repository, []DetailSource{source}, time.Hour)
	results := make(chan error, 2)
	go func() { _, err := service.Refresh(context.Background(), 2); results <- err }()
	<-started
	go func() { _, err := service.Refresh(context.Background(), 2); results <- err }()
	close(release)
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.calls != 1 {
		t.Fatalf("detail calls=%d", source.calls)
	}
}

func TestDetailsServicePreservesFailureAndRejectsUnsupported(t *testing.T) {
	repository := &detailsRepository{record: ListingRecord{ID: 3, Listing: model.Listing{Source: "dccolombia", ExternalID: "C", DetailsStatus: "READY"}}}
	service := NewDetailsService(repository, nil, time.Hour)
	if _, err := service.Refresh(context.Background(), 3); !errors.Is(err, ErrDetailsUnsupported) {
		t.Fatalf("err=%v", err)
	}
	if repository.record.Listing.DetailsStatus != "UNSUPPORTED" {
		t.Fatalf("status=%s", repository.record.Listing.DetailsStatus)
	}
}

func TestDetailsServiceRateLimitsRepeatedFailedRequests(t *testing.T) {
	repository := &detailsRepository{record: ListingRecord{ID: 4, Listing: model.Listing{Source: "vixora", ExternalID: "D", DetailsStatus: "NOT_LOADED"}}}
	source := &detailsSource{fn: func() (model.Listing, error) { return model.Listing{}, errors.New("upstream unavailable") }}
	service := NewDetailsService(repository, []DetailSource{source}, time.Hour)
	service.MinRetryInterval = time.Hour
	if _, err := service.Refresh(context.Background(), 4); err == nil {
		t.Fatal("expected first request to fail")
	}
	if _, err := service.Refresh(context.Background(), 4); !errors.Is(err, ErrDetailsRateLimited) {
		t.Fatalf("err=%v", err)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.calls != 1 {
		t.Fatalf("detail calls=%d", source.calls)
	}
}
