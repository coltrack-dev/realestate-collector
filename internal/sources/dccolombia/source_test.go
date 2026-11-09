package dccolombia

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/example/realestate-collector/internal/sources/httpclient"
)

func TestCollectParsesDocumentationFixture(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/fixtures/dccolombia/documentation_example.json")
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})
	source := Source{Client: httpclient.New(&http.Client{Transport: transport}, "test", 0), Endpoint: "https://fixture.invalid/properties"}
	listings, err := source.Collect(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 1 || listings[0].ExternalID != "wasi-10018216" {
		t.Fatalf("unexpected listings: %#v", listings)
	}
	listing := listings[0]
	if listing.Operation != "RENT" || listing.Price == nil || *listing.Price != 2800000 || listing.Currency != "COP" {
		t.Fatalf("rent price not parsed: %#v", listing)
	}
	if listing.URL != "https://www.dccolombia.com.co/inmueble/wasi-10018216" {
		t.Fatalf("unexpected canonical URL: %s", listing.URL)
	}
}

func TestLiveJSONFixtureHasRecords(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/fixtures/dccolombia/live_properties.json")
	if err != nil {
		t.Fatal(err)
	}
	var page response
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	if page.Properties == nil {
		t.Fatal("saved live JSON has no properties field")
	}
	if len(*page.Properties) != 200 {
		t.Fatalf("unexpected saved live JSON records: %d", len(*page.Properties))
	}
	for _, item := range *page.Properties {
		if _, err := toListing(item); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCollectForRefreshDistinguishesEmptyResponse(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(`{"properties":[]}`))}, nil
	})
	source := Source{Client: httpclient.New(&http.Client{Transport: transport}, "test", 0), Endpoint: "https://fixture.invalid/properties"}
	result, err := source.CollectForRefresh(context.Background(), 10)
	if err != nil || result.FetchedCount != 0 || !result.Complete {
		t.Fatalf("empty response should be successful and complete: %+v, %v", result, err)
	}
}

func TestCollectMapsCoordinatorFiltersToProviderValues(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("city") != "Cartagena" || query.Get("operation") != "sale" || query.Get("property_type") != "apartment" {
			t.Fatalf("unexpected D.C Colombia query: %s", request.URL.RawQuery)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(`{"properties":[{"slug":"cartagena-1","city":"Cartagena de Indias","operation":"sale","property_type":"apartment","price_sale":175000000}]}`))}, nil
	})
	source := Source{Client: httpclient.New(&http.Client{Transport: transport}, "test", 0), Endpoint: "https://fixture.invalid/properties", City: "Cartagena", Operation: "SALE", PropertyType: "APARTMENT"}
	result, err := source.CollectForRefresh(context.Background(), 10)
	if err != nil || result.FetchedCount != 1 || result.Listings[0].City != "cartagena de indias" {
		t.Fatalf("unexpected mapped Cartagena result: %+v, %v", result, err)
	}
}

func TestCollectAllowsALLFiltersWithoutAddingUnsupportedValues(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("city") != "Cartagena" || query.Get("operation") != "" || query.Get("property_type") != "" {
			t.Fatalf("unexpected ALL query: %s", request.URL.RawQuery)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(`{"properties":[]}`))}, nil
	})
	source := Source{Client: httpclient.New(&http.Client{Transport: transport}, "test", 0), Endpoint: "https://fixture.invalid/properties", City: "Cartagena"}
	result, err := source.CollectForRefresh(context.Background(), 10)
	if err != nil || result.FetchedCount != 0 || !result.Complete {
		t.Fatalf("unexpected ALL result: %+v, %v", result, err)
	}
}

func TestCollectForRefreshRejectsMissingPropertiesField(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(`{"data":[]}`))}, nil
	})
	source := Source{Client: httpclient.New(&http.Client{Transport: transport}, "test", 0), Endpoint: "https://fixture.invalid/properties"}
	if _, err := source.CollectForRefresh(context.Background(), 10); err == nil {
		t.Fatal("missing properties field should be treated as a schema error")
	}
}

func TestLiveCSVFixtureHasRows(t *testing.T) {
	file, err := os.Open("../../../testdata/fixtures/dccolombia/live_properties.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 51 || len(rows[0]) != 13 {
		t.Fatalf("unexpected saved live CSV: rows=%d columns=%d", len(rows), len(rows[0]))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
