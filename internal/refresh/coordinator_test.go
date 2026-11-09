package refresh

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/example/realestate-collector/internal/model"
)

type testSource struct {
	name  string
	calls *int
	mu    *sync.Mutex
}

func (s testSource) Name() string { return s.name }
func (s testSource) Collect(context.Context, Request, int) ([]model.Listing, error) {
	s.mu.Lock()
	*s.calls++
	s.mu.Unlock()
	return []model.Listing{{Source: s.name, ExternalID: "one", URL: "https://example.test/one", Country: "Colombia", CountryCode: "CO"}}, nil
}

type testSaver struct {
	mu    sync.Mutex
	saved int
}

func (s *testSaver) Save(context.Context, model.Listing) (bool, bool, error) {
	s.mu.Lock()
	s.saved++
	s.mu.Unlock()
	return true, false, nil
}

func testRequest() Request {
	return Request{CountryCode: "CO", City: "Bogotá", Bounds: Bounds{MinLongitude: -74.2, MaxLongitude: -74.0, MinLatitude: 4.6, MaxLatitude: 4.8}, Operation: "SALE", PropertyType: "APARTMENT"}
}

func TestCoordinatorDeduplicatesAndCachesRefresh(t *testing.T) {
	var calls int
	var mu sync.Mutex
	saver := &testSaver{}
	c := NewCoordinator(saver, []Registration{{Name: "test", Capabilities: Capabilities{CountryCode: "CO", SupportsCity: true, AllowsAutomatedRefresh: true, CacheTTL: time.Minute}, Factory: func(Request) Source { return testSource{name: "test", calls: &calls, mu: &mu} }}})
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
