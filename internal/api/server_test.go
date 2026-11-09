package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/realestate-collector/internal/model"
)

type fakeRepository struct {
	Records    []ListingRecord
	Filter     ListingFilter
	StatsValue Stats
	PingErr    error
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

func pointer(value int64) *int64 { return &value }
