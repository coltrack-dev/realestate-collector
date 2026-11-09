package refresh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/sources/httpclient"
)

type testSource struct {
	name     string
	calls    *int
	mu       *sync.Mutex
	complete bool
}

func (s testSource) Name() string { return s.name }
func (s testSource) Collect(context.Context, Request, int) (CollectResult, error) {
	s.mu.Lock()
	*s.calls++
	s.mu.Unlock()
	return CollectResult{Listings: []model.Listing{{Source: s.name, ExternalID: "one", URL: "https://example.test/one", Country: "Colombia", CountryCode: "CO"}}, FetchedCount: 1, Complete: s.complete}, nil
}

type testSaver struct {
	mu    sync.Mutex
	saved int
	fail  bool
}

type errorSource struct{ name string }

func (s errorSource) Name() string { return s.name }
func (s errorSource) Collect(context.Context, Request, int) (CollectResult, error) {
	return CollectResult{}, errors.New("mock provider failure")
}

type memorySaver struct {
	mu       sync.Mutex
	listings map[string]model.Listing
}

func (s *memorySaver) Save(_ context.Context, listing model.Listing) (bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listings == nil {
		s.listings = make(map[string]model.Listing)
	}
	key := listing.Source + ":" + listing.ExternalID
	previous, exists := s.listings[key]
	if !exists {
		s.listings[key] = listing
		return true, false, nil
	}
	updated := previous.Price == nil || listing.Price == nil || *previous.Price != *listing.Price
	s.listings[key] = listing
	return false, updated, nil
}

func (s *testSaver) Save(context.Context, model.Listing) (bool, bool, error) {
	s.mu.Lock()
	s.saved++
	s.mu.Unlock()
	if s.fail {
		return false, false, errors.New("isolated save failure")
	}
	return true, false, nil
}

func testRequest() Request {
	return Request{CountryCode: "CO", City: "Bogotá", Bounds: Bounds{MinLongitude: -74.2, MaxLongitude: -74.0, MinLatitude: 4.6, MaxLatitude: 4.8}, Operation: "SALE", PropertyType: "APARTMENT"}
}

func TestCoordinatorDeduplicatesAndCachesRefresh(t *testing.T) {
	var calls int
	var mu sync.Mutex
	saver := &testSaver{}
	c := NewCoordinator(saver, []Registration{{Name: "test", Capabilities: Capabilities{CountryCode: "CO", SupportsBBox: true, SupportsCity: true, AllowsAutomatedRefresh: true, CacheTTL: time.Minute}, Factory: func(Request) Source { return testSource{name: "test", calls: &calls, mu: &mu, complete: true} }}})
	defer c.Close()
	first := c.Request(testRequest(), "client-a")
	if first.Status != StatusQueued {
		t.Fatalf("first status=%s", first.Status)
	}
	second := c.Request(testRequest(), "client-b")
	if second.RefreshID != first.RefreshID {
		t.Fatalf("duplicate refresh IDs: %q and %q", first.RefreshID, second.RefreshID)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, ok := c.Get(first.RefreshID)
		if ok && status.Status == StatusDone {
			break
		}
		time.Sleep(time.Millisecond * 10)
	}
	status, _ := c.Get(first.RefreshID)
	if status.Status != StatusDone {
		t.Fatalf("status=%s", status.Status)
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("source calls=%d, want 1", gotCalls)
	}
	if result := c.Request(testRequest(), "client-c"); result.Status != StatusSkipped {
		t.Fatalf("cached status=%s", result.Status)
	}
}

func TestCoordinatorDoesNotCallDisabledSource(t *testing.T) {
	called := false
	c := NewCoordinator(&testSaver{}, []Registration{{Name: "disabled", Capabilities: Capabilities{CountryCode: "UY", SupportsCity: true}, Factory: func(Request) Source {
		called = true
		return testSource{name: "disabled", calls: new(int), mu: &sync.Mutex{}}
	}}})
	defer c.Close()
	result := c.Request(Request{CountryCode: "UY", City: "Montevideo", Bounds: testRequest().Bounds}, "client")
	if result.Status != StatusUnsupported || called {
		t.Fatalf("result=%+v called=%v", result, called)
	}
}

func TestCoordinatorPlansUnfilteredSearchOnlyForCapableSources(t *testing.T) {
	var calls int
	var allCalls int
	var filteredCalls int
	var mu sync.Mutex
	c := NewCoordinator(&testSaver{}, []Registration{
		{Name: "unsupported-all", Capabilities: Capabilities{CountryCode: "CO", SupportsCity: true, AllowsAutomatedRefresh: true}, Factory: func(Request) Source {
			mu.Lock()
			filteredCalls++
			mu.Unlock()
			return testSource{name: "unsupported-all", calls: &calls, mu: &mu, complete: false}
		}},
		{Name: "all-capable", Capabilities: Capabilities{CountryCode: "CO", SupportsCity: true, SupportsUnfilteredSearch: true, AllowsAutomatedRefresh: true}, Factory: func(Request) Source {
			return testSource{name: "all-capable", calls: &allCalls, mu: &mu, complete: false}
		}},
	})
	defer c.Close()
	for index, filters := range []struct{ operation, propertyType string }{
		{operation: "", propertyType: ""},
		{operation: "SALE", propertyType: ""},
		{operation: "", propertyType: "APARTMENT"},
		{operation: "SALE", propertyType: "APARTMENT"},
	} {
		request := testRequest()
		request.Operation = filters.operation
		request.PropertyType = filters.propertyType
		result := c.Request(request, fmt.Sprintf("all-client-%d", index))
		if result.Status != StatusQueued {
			t.Fatalf("filters=%+v status=%s, want QUEUED", filters, result.Status)
		}
		waitForStatus(t, c, result.RefreshID, StatusPartial)
	}
	mu.Lock()
	got := allCalls
	mu.Unlock()
	if got != 4 {
		t.Fatalf("planned source calls=%d, want 4", got)
	}
	mu.Lock()
	filtered := filteredCalls
	mu.Unlock()
	if filtered != 1 {
		t.Fatalf("filtered-only source calls=%d, want 1 concrete-filter call", filtered)
	}
}

func TestCoordinatorRejectsALLWhenNoSourceSupportsIt(t *testing.T) {
	c := NewCoordinator(&testSaver{}, []Registration{{Name: "filtered-only", Capabilities: Capabilities{CountryCode: "CO", SupportsCity: true, AllowsAutomatedRefresh: true}, Factory: func(Request) Source {
		return testSource{name: "filtered-only", calls: new(int), mu: &sync.Mutex{}}
	}}})
	defer c.Close()
	request := testRequest()
	request.Operation = ""
	request.PropertyType = ""
	if result := c.Request(request, "all-client"); result.Status != StatusUnsupported {
		t.Fatalf("ALL unsupported status=%s, want UNSUPPORTED", result.Status)
	}
}

func TestCoordinatorIdentityIncludesAppliedFilters(t *testing.T) {
	base := testRequest()
	if requestIdentityFor(base, "test") == requestIdentityFor(Request{CountryCode: "CO", City: base.City, Bounds: base.Bounds, Operation: "RENT", PropertyType: base.PropertyType}, "test") {
		t.Fatal("operation was omitted from identity")
	}
	if requestIdentityFor(base, "test") == requestIdentityFor(Request{CountryCode: "UY", City: base.City, Bounds: base.Bounds, Operation: base.Operation, PropertyType: base.PropertyType}, "test") {
		t.Fatal("country was omitted from identity")
	}
	if requestIdentityFor(base, "test") == requestIdentityFor(Request{CountryCode: base.CountryCode, City: base.City, Bounds: base.Bounds, Operation: base.Operation, PropertyType: "HOUSE"}, "test") {
		t.Fatal("property type was omitted from identity")
	}
}

func TestCoordinatorDoesNotCachePartialOrFailedSave(t *testing.T) {
	for _, test := range []struct {
		name           string
		sourceComplete bool
		saverFails     bool
	}{{"partial", false, false}, {"save failure", true, true}} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			var mu sync.Mutex
			saver := &testSaver{fail: test.saverFails}
			c := NewCoordinator(saver, []Registration{{Name: "test", Capabilities: Capabilities{CountryCode: "CO", SupportsCity: true, AllowsAutomatedRefresh: true, CacheTTL: time.Minute}, Factory: func(Request) Source {
				return testSource{name: "test", calls: &calls, mu: &mu, complete: test.sourceComplete}
			}}})
			defer c.Close()
			first := c.Request(testRequest(), "one")
			waitForStatus(t, c, first.RefreshID, StatusPartial, StatusFailed)
			second := c.Request(testRequest(), "two")
			if second.Status != StatusQueued {
				t.Fatalf("second status=%s, expected a new job", second.Status)
			}
		})
	}
}

func waitForStatus(t *testing.T, coordinator *Coordinator, id string, wanted ...Status) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, ok := coordinator.Get(id)
		if ok {
			for _, candidate := range wanted {
				if status.Status == candidate {
					return
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	status, _ := coordinator.Get(id)
	t.Fatalf("job %q ended with status %s", id, status.Status)
}

type httpBackedSource struct {
	endpoint string
	calls    *int
	mu       *sync.Mutex
}

func (s httpBackedSource) Name() string { return "mock-source" }
func (s httpBackedSource) Collect(ctx context.Context, _ Request, _ int) (CollectResult, error) {
	s.mu.Lock()
	*s.calls++
	s.mu.Unlock()
	client := httpclient.New(&http.Client{}, "test", 0)
	client.SetSource("mock-source")
	var payload struct {
		OK bool `json:"ok"`
	}
	if err := client.GetJSON(ctx, s.endpoint, &payload); err != nil {
		return CollectResult{}, err
	}
	return CollectResult{FetchedCount: 1, Complete: payload.OK}, nil
}

func TestCoordinatorExecutesAdapterHTTPRequest(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	var calls int
	var mu sync.Mutex
	coordinator := NewCoordinator(&testSaver{}, []Registration{{Name: "mock-source", Capabilities: Capabilities{CountryCode: "CO", SupportsBBox: true, AllowsAutomatedRefresh: true}, Factory: func(Request) Source { return httpBackedSource{endpoint: server.URL, calls: &calls, mu: &mu} }}})
	defer coordinator.Close()
	result := coordinator.Request(testRequest(), "mock-client")
	waitForStatus(t, coordinator, result.RefreshID, StatusDone)
	if requests != 1 || calls != 1 {
		t.Fatalf("external requests=%d adapter calls=%d, want one each", requests, calls)
	}
}

func TestCoordinatorKeepsSuccessfulSourceWhenAnotherFails(t *testing.T) {
	good := testSource{name: "good", calls: new(int), mu: &sync.Mutex{}, complete: true}
	c := NewCoordinator(&testSaver{}, []Registration{
		{Name: "bad", Capabilities: Capabilities{CountryCode: "CO", SupportsBBox: true, AllowsAutomatedRefresh: true}, Factory: func(Request) Source { return errorSource{name: "bad"} }},
		{Name: "good", Capabilities: Capabilities{CountryCode: "CO", SupportsBBox: true, AllowsAutomatedRefresh: true}, Factory: func(Request) Source { return good }},
	})
	defer c.Close()
	result := c.Request(testRequest(), "mixed-source-client")
	waitForStatus(t, c, result.RefreshID, StatusPartial)
	status, _ := c.Get(result.RefreshID)
	if status.Status != StatusPartial || status.Error == "" {
		t.Fatalf("mixed result lost source failure: %+v", status)
	}
	if *good.calls != 1 {
		t.Fatalf("successful source was not executed: %d", *good.calls)
	}
}

func TestCoordinatorReportsInsertedUnchangedAndUpdatedListings(t *testing.T) {
	price := int64(100)
	changedPrice := int64(125)
	listing := model.Listing{Source: "mock", ExternalID: "one", URL: "https://example.test/one", Price: &price}
	updated := listing
	updated.Price = &changedPrice
	current := make([]model.Listing, 10)
	for index := range current {
		current[index] = listing
		current[index].ExternalID = fmt.Sprintf("listing-%d", index)
		current[index].URL = "https://example.test/" + current[index].ExternalID
	}
	saver := &memorySaver{}
	c := NewCoordinator(saver, []Registration{{Name: "mock", Capabilities: Capabilities{CountryCode: "CO", SupportsCity: true, AllowsAutomatedRefresh: true}, Factory: func(Request) Source {
		items := append([]model.Listing(nil), current...)
		return fixedSource{name: "mock", listings: items}
	}}})
	defer c.Close()
	first := c.Request(testRequest(), "counter-1")
	waitForStatus(t, c, first.RefreshID, StatusPartial)
	status, _ := c.Get(first.RefreshID)
	if status.ListingsInserted != 10 || status.ListingsUpdated != 0 || status.ListingsUnchanged != 0 {
		t.Fatalf("unexpected first counters: %+v", status)
	}
	current = append([]model.Listing(nil), current...)
	second := c.Request(testRequest(), "counter-2")
	waitForStatus(t, c, second.RefreshID, StatusPartial)
	status, _ = c.Get(second.RefreshID)
	if status.ListingsInserted != 0 || status.ListingsUpdated != 0 || status.ListingsUnchanged != 10 || len(saver.listings) != 10 {
		t.Fatalf("unexpected unchanged counters: %+v listings=%d", status, len(saver.listings))
	}
	current = []model.Listing{updated}
	current[0].ExternalID = "listing-0"
	current[0].URL = "https://example.test/listing-0"
	third := c.Request(testRequest(), "counter-3")
	waitForStatus(t, c, third.RefreshID, StatusPartial)
	status, _ = c.Get(third.RefreshID)
	if status.ListingsInserted != 0 || status.ListingsUpdated != 1 || status.ListingsUnchanged != 0 || len(saver.listings) != 10 {
		t.Fatalf("unexpected updated counters: %+v listings=%d", status, len(saver.listings))
	}
}

func TestCoordinatorEmptySourceDoesNotChangeInventory(t *testing.T) {
	saver := &memorySaver{}
	c := NewCoordinator(saver, []Registration{{Name: "empty", Capabilities: Capabilities{CountryCode: "CO", SupportsCity: true, AllowsAutomatedRefresh: true}, Factory: func(Request) Source {
		return fixedSource{name: "empty", listings: nil}
	}}})
	defer c.Close()
	result := c.Request(testRequest(), "empty-source")
	waitForStatus(t, c, result.RefreshID, StatusPartial)
	status, _ := c.Get(result.RefreshID)
	if status.ListingsFetched != 0 || status.ListingsInserted != 0 || len(saver.listings) != 0 {
		t.Fatalf("empty source changed inventory: %+v listings=%d", status, len(saver.listings))
	}
}

type fixedSource struct {
	name     string
	listings []model.Listing
}

func (s fixedSource) Name() string { return s.name }
func (s fixedSource) Collect(context.Context, Request, int) (CollectResult, error) {
	return CollectResult{Listings: s.listings, FetchedCount: len(s.listings), Complete: false, PartialReason: "city_pagination_unknown"}, nil
}
