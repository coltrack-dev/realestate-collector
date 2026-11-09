package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/realestate-collector/internal/geo"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/refresh"
)

type fakeRepository struct {
	Records    []ListingRecord
	Filter     ListingFilter
	StatsValue Stats
	PingErr    error
}

type pipelineRepository struct {
	mu      sync.Mutex
	records []ListingRecord
}

func (p *pipelineRepository) Ping(context.Context) error { return nil }
func (p *pipelineRepository) List(_ context.Context, filter ListingFilter) ([]ListingRecord, int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := append([]ListingRecord(nil), p.records...)
	if filter.Offset >= len(result) {
		return nil, int64(len(result)), nil
	}
	result = result[filter.Offset:]
	if len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, int64(len(p.records)), nil
}
func (p *pipelineRepository) Get(_ context.Context, id int64) (ListingRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, record := range p.records {
		if record.ID == id {
			return record, nil
		}
	}
	return ListingRecord{}, ErrNotFound
}
func (p *pipelineRepository) History(context.Context, int64) ([]Snapshot, error) { return nil, nil }
func (p *pipelineRepository) Stats(context.Context) (Stats, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	stats := Stats{BySource: map[string]int64{}, ByOperation: map[string]int64{}}
	stats.Total = int64(len(p.records))
	for _, record := range p.records {
		stats.BySource[record.Listing.Source]++
		stats.ByOperation[record.Listing.Operation]++
	}
	return stats, nil
}
func (p *pipelineRepository) Save(_ context.Context, listing model.Listing) (bool, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for index := range p.records {
		if p.records[index].Listing.Source == listing.Source && p.records[index].Listing.ExternalID == listing.ExternalID {
			p.records[index].Listing = listing
			return false, true, nil
		}
	}
	p.records = append(p.records, ListingRecord{ID: int64(len(p.records) + 1), Listing: listing})
	return true, false, nil
}

func (f *fakeRepository) Ping(context.Context) error { return f.PingErr }
func (f *fakeRepository) List(_ context.Context, filter ListingFilter) ([]ListingRecord, int64, error) {
	f.Filter = filter
	return f.Records, int64(len(f.Records)), nil
}
func (f *fakeRepository) Get(_ context.Context, id int64) (ListingRecord, error) {
	for _, record := range f.Records {
		if record.ID == id {
			return record, nil
		}
	}
	return ListingRecord{}, ErrNotFound
}
func (f *fakeRepository) History(context.Context, int64) ([]Snapshot, error) {
	return []Snapshot{{ID: 5, Price: pointer(int64(100)), CollectedAt: time.Unix(1, 0).UTC()}}, nil
}
func (f *fakeRepository) Stats(context.Context) (Stats, error) { return f.StatsValue, nil }

func TestListHandlerParsesFiltersAndKeepsNulls(t *testing.T) {
	repository := &fakeRepository{Records: []ListingRecord{{ID: 7, Listing: model.Listing{Source: "vixora", ExternalID: "FIN-1", Price: nil}}}}
	handler := NewServer(repository)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/listings?source=vixora&city=Bogotá%20D.C.&operation=SALE&property_type=APARTMENT&min_price=10&max_area=90&min_longitude=-74.2&max_longitude=-74.0&min_latitude=4.6&max_latitude=4.8&limit=20&offset=5", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	if repository.Filter.Limit != 20 || repository.Filter.Offset != 5 || repository.Filter.MinPrice == nil || *repository.Filter.MinPrice != 10 || repository.Filter.MinLongitude == nil || *repository.Filter.MinLongitude != -74.2 || repository.Filter.MaxLatitude == nil || *repository.Filter.MaxLatitude != 4.8 {
		t.Fatalf("unexpected filter: %#v", repository.Filter)
	}
	var body map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["complete"] != true {
		t.Fatalf("expected complete response: %#v", body["complete"])
	}
	data := body["data"].([]any)
	listing := data[0].(map[string]any)
	if listing["price"] != nil {
		t.Fatalf("nullable price was omitted or changed: %#v", listing["price"])
	}
	if listing["id"] != float64(7) {
		t.Fatalf("missing id: %#v", listing["id"])
	}
}

func TestListingHistoryAndErrors(t *testing.T) {
	repository := &fakeRepository{Records: []ListingRecord{{ID: 7}}}
	handler := NewServer(repository)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/listings/7/history", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/listings/not-an-id", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/listings/99", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d", recorder.Code)
	}
}

func TestHealthAndReadOnlyMethods(t *testing.T) {
	repository := &fakeRepository{PingErr: errors.New("offline")}
	handler := NewServer(repository)
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/listings", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status=%d allow=%q", recorder.Code, recorder.Header().Get("Allow"))
	}
}

func TestGeoResolveEndpointReturnsAdministrativeResult(t *testing.T) {
	resolver, err := geo.NewFromReader(strings.NewReader(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"country_code":"CO","department":"Valle del Cauca","municipality":"Cali"},"geometry":{"type":"Polygon","coordinates":[[[-76,3],[-76,4],[-75,4],[-75,3],[-76,3]]]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServerWithGeo(&fakeRepository{}, nil, nil, resolver)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/geo/resolve?longitude=-75.5&latitude=3.5", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	var result geo.Result
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.CountryCode != "CO" || result.Municipality != "Cali" || result.Resolution != geo.AdministrativePolygon {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGeoResolveViewportEndpointReturnsAllIntersectingMunicipalities(t *testing.T) {
	resolver, err := geo.NewFromReader(strings.NewReader(`{"type":"FeatureCollection","features":[
{"type":"Feature","properties":{"country_code":"CO","department":"Antioquia","municipality":"Rionegro","divipola":"05615"},"geometry":{"type":"Polygon","coordinates":[[[-76,6],[-75,6],[-75,7],[-76,7],[-76,6]]]}},
{"type":"Feature","properties":{"country_code":"CO","department":"Antioquia","municipality":"Marinilla","divipola":"05440"},"geometry":{"type":"Polygon","coordinates":[[[-75,6],[-74.5,6],[-74.5,7],[-75,7],[-75,6]]]}}
]}`))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServerWithGeo(&fakeRepository{}, nil, nil, resolver)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/geo/resolve-viewport", strings.NewReader(`{"bbox":{"min_longitude":-75.2,"max_longitude":-74.8,"min_latitude":6.1,"max_latitude":6.4}}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result geo.ViewportResult
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Municipalities) != 2 {
		t.Fatalf("unexpected viewport result: %+v", result)
	}
}

func TestSourceRefreshIsDisabledWhenNoSourceIsPermitted(t *testing.T) {
	repository := &fakeRepository{}
	coordinator := refresh.NewCoordinator(nil, nil)
	defer coordinator.Close()
	handler := NewServerWithRefresh(repository, nil, coordinator)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/source-refresh", strings.NewReader(`{"country_code":"CO","city":"Bogota","operation":"SALE","property_type":"APARTMENT","bbox":{"min_longitude":-74.2,"max_longitude":-74.0,"min_latitude":4.6,"max_latitude":4.8}}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestValidateSourceRefreshAllowsALLFilters(t *testing.T) {
	payload := sourceRefreshRequest{
		CountryCode: "CO",
		City:        "Cartagena",
		Bounds:      refresh.Bounds{MinLongitude: -75.7, MaxLongitude: -75.4, MinLatitude: 10.3, MaxLatitude: 10.6},
	}
	if err := validateSourceRefresh(payload); err != nil {
		t.Fatalf("empty operation and property type should mean ALL: %v", err)
	}
	payload.Operation = "SALE"
	if err := validateSourceRefresh(payload); err != nil {
		t.Fatalf("SALE with ALL property type should be valid: %v", err)
	}
	payload.Operation = ""
	payload.PropertyType = "APARTMENT"
	if err := validateSourceRefresh(payload); err != nil {
		t.Fatalf("ALL operation with APARTMENT should be valid: %v", err)
	}
}

type refreshErrorSource struct{}

func (refreshErrorSource) Name() string { return "test" }
func (refreshErrorSource) Collect(context.Context, refresh.Request, int) (refresh.CollectResult, error) {
	return refresh.CollectResult{}, errors.New("upstream https://api.example.test/search?token=secret response body=private")
}

type pipelineSource struct{}

func (pipelineSource) Name() string { return "mock-cartagena" }
func (pipelineSource) Collect(context.Context, refresh.Request, int) (refresh.CollectResult, error) {
	return refresh.CollectResult{
		Listings:     []model.Listing{{Source: "mock-cartagena", ExternalID: "CART-1", URL: "https://example.test/CART-1", Title: "Apartamento Cartagena", Operation: "SALE", PropertyType: "APARTMENT", Country: "Colombia", CountryCode: "CO", City: "Cartagena de Indias"}},
		FetchedCount: 1,
		Complete:     true,
	}, nil
}

func TestRefreshPipelineReachesListingsAndStats(t *testing.T) {
	repository := &pipelineRepository{}
	coordinator := refresh.NewCoordinator(repository, []refresh.Registration{{
		Name:         "mock-cartagena",
		Capabilities: refresh.Capabilities{CountryCode: "CO", SupportsBBox: true, SupportsUnfilteredSearch: true, AllowsAutomatedRefresh: true},
		Factory:      func(refresh.Request) refresh.Source { return pipelineSource{} },
	}})
	defer coordinator.Close()
	handler := NewServerWithRefresh(repository, nil, coordinator)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/source-refresh", strings.NewReader(`{"country_code":"CO","city":"Cartagena","operation":"SALE","property_type":"APARTMENT","bbox":{"min_longitude":-75.7,"max_longitude":-75.4,"min_latitude":10.3,"max_latitude":10.6}}`))
	request.Header.Set("Content-Type", "application/json")
	queuedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(queuedRecorder, request)
	if queuedRecorder.Code != http.StatusAccepted {
		t.Fatalf("refresh status=%d body=%s", queuedRecorder.Code, queuedRecorder.Body.String())
	}
	var queued refresh.Result
	if err := json.NewDecoder(queuedRecorder.Body).Decode(&queued); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, ok := coordinator.Get(queued.RefreshID)
		if ok && status.Status == refresh.StatusDone {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	status, _ := coordinator.Get(queued.RefreshID)
	if status.Status != refresh.StatusDone || status.ListingsInserted != 1 {
		t.Fatalf("unexpected pipeline status: %+v", status)
	}
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/listings?city=Cartagena%20de%20Indias", nil))
	if listRecorder.Code != http.StatusOK || !strings.Contains(listRecorder.Body.String(), "CART-1") {
		t.Fatalf("listing was not exposed by API: status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	statsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statsRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil))
	if statsRecorder.Code != http.StatusOK || !strings.Contains(statsRecorder.Body.String(), `"total":1`) {
		t.Fatalf("stats did not increase: status=%d body=%s", statsRecorder.Code, statsRecorder.Body.String())
	}
}

func TestSourceRefreshStatusHidesInternalError(t *testing.T) {
	coordinator := refresh.NewCoordinator(nil, []refresh.Registration{{Name: "test", Capabilities: refresh.Capabilities{CountryCode: "CO", SupportsBBox: true, AllowsAutomatedRefresh: true}, Factory: func(refresh.Request) refresh.Source { return refreshErrorSource{} }}})
	defer coordinator.Close()
	handler := NewServerWithRefresh(&fakeRepository{}, nil, coordinator)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/source-refresh", strings.NewReader(`{"country_code":"CO","city":"Bogota","operation":"SALE","property_type":"APARTMENT","bbox":{"min_longitude":-74.2,"max_longitude":-74.0,"min_latitude":4.6,"max_latitude":4.8}}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var queued refresh.Result
	if err := json.NewDecoder(recorder.Body).Decode(&queued); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, ok := coordinator.Get(queued.RefreshID)
		if ok && status.Status == refresh.StatusFailed {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/source-refresh/"+queued.RefreshID, nil)
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusRequest)
	if strings.Contains(statusRecorder.Body.String(), "token") || strings.Contains(statusRecorder.Body.String(), "private") || strings.Contains(statusRecorder.Body.String(), "api.example.test") {
		t.Fatalf("internal error leaked: %s", statusRecorder.Body.String())
	}
}

func pointer(value int64) *int64 { return &value }
