package vixora

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/example/realestate-collector/internal/sources/httpclient"
)

func TestCollectParsesDocumentationFixture(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/fixtures/vixora/documentation_example.json")
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("page_size") != "1" {
			t.Fatalf("unexpected page_size: %s", r.URL.RawQuery)
		}
		return jsonResponse(body), nil
	})
	source := Source{Client: httpclient.New(&http.Client{Transport: transport}, "test", 0), Endpoint: "https://fixture.invalid/properties"}
	listings, err := source.Collect(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 1 || listings[0].ExternalID != "FIN-0012" {
		t.Fatalf("unexpected listings: %#v", listings)
	}
	if listings[0].Price == nil || *listings[0].Price != 850000000 || listings[0].Currency != "COP" {
		t.Fatalf("price not parsed: %#v", listings[0])
	}
	if listings[0].Latitude == nil || *listings[0].Latitude != 6.153 {
		t.Fatalf("coordinates not parsed: %#v", listings[0])
	}
}

func TestCollectStopsAtPaginationEnd(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		result := response{Data: []property{{Code: "FIN-1", URL: "https://vixora.test/1", Type: "finca", Operation: "venta"}}}
		result.Pagination.TotalPages = 2
		if page < 1 {
			t.Fatalf("invalid page: %d", page)
		}
		encoded, _ := json.Marshal(result)
		return jsonResponse(encoded), nil
	})
	source := Source{Client: httpclient.New(&http.Client{Transport: transport}, "test", 0), Endpoint: "https://fixture.invalid/properties"}
	listings, err := source.Collect(context.Background(), 3)
	if err != nil || len(listings) != 2 {
		t.Fatalf("unexpected pagination result: %d, %v", len(listings), err)
	}
}

func TestLiveFixtureHasPublishedRecords(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/fixtures/vixora/live_page_1.json")
	if err != nil { t.Fatal(err) }
	var page response
	if err := json.Unmarshal(body, &page); err != nil { t.Fatal(err) }
	if len(page.Data) != 30 || page.Pagination.TotalPages != 1 {
		t.Fatalf("unexpected saved live fixture: records=%d pages=%d", len(page.Data), page.Pagination.TotalPages)
	}
	for _, item := range page.Data {
		if item.Status != "published" { t.Fatalf("unpublished item in live fixture: %s", item.Code) }
		if _, err := toListing(item); err != nil { t.Fatal(err) }
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func jsonResponse(body []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}
