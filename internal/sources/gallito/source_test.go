package gallito

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
)

func TestCollectUsesSearchSummaryWithoutDetails(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/fixtures/gallito/search_montevideo_sale_apartment.json")
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var call struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Name != "search_listings" {
			t.Fatalf("summary collection called %s", call.Params.Name)
		}
		return rpcResponse(body), nil
	})
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	listings, err := source.Collect(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 10 {
		t.Fatalf("listings=%d", len(listings))
	}
	if listings[0].Country != "Uruguay" || listings[0].Currency != "USD" || listings[0].AreaM2 != nil {
		t.Fatalf("unexpected summary mapping: %#v", listings[0])
	}
	if listings[0].DetailsStatus != "NOT_LOADED" {
		t.Fatalf("status=%q", listings[0].DetailsStatus)
	}
}

func TestFetchDetailsPreservesDescriptionAndPhotoOrder(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/fixtures/gallito/detail_6aeff39935abd95ffb2166d8.json")
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var call struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Name != "get_listing" {
			t.Fatalf("unexpected method %s", call.Params.Name)
		}
		return rpcResponse(body), nil
	})
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	listing, err := source.FetchDetails(context.Background(), "6aeff39935abd95ffb2166d8")
	if err != nil {
		t.Fatal(err)
	}
	if listing.Description == "" || len(listing.Images) != 8 || listing.Images[0] == listing.Images[7] {
		t.Fatalf("unexpected detail mapping: %#v", listing)
	}
}

func TestCollectStopsAtConfiguredLimit(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/fixtures/gallito/search_montevideo_sale_apartment.json")
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) { return rpcResponse(body), nil })
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	listings, err := source.Collect(context.Background(), 3)
	if err != nil || len(listings) != 3 {
		t.Fatalf("listings=%d err=%v", len(listings), err)
	}
}

func TestCollectUsesMoreResultsPagination(t *testing.T) {
	first := []byte(`{"searchId":"search-1234","page":1,"totalPages":2,"total":2,"listings":[{"id":"a1","title":"A","operation":"sale","propertyType":"apartment","url":"https://gallito.test/a"}]}`)
	second := []byte(`{"searchId":"search-1234","page":2,"totalPages":2,"total":2,"listings":[{"id":"a2","title":"B","operation":"sale","propertyType":"apartment","url":"https://gallito.test/b"}]}`)
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var call struct {
			Params struct {
				Name      string `json:"name"`
				Arguments struct {
					Page int `json:"page"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Name == "search_listings" {
			return rpcResponse(first), nil
		}
		if call.Params.Name == "more_results" && call.Params.Arguments.Page == 2 {
			return rpcResponse(second), nil
		}
		t.Fatalf("unexpected paginated call: %+v", call)
		return nil, nil
	})
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	listings, err := source.Collect(context.Background(), 2)
	if err != nil || len(listings) != 2 || listings[1].ExternalID != "a2" {
		t.Fatalf("listings=%#v err=%v", listings, err)
	}
}

func TestCollectJobsDeduplicatesAndPersistsProgress(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/fixtures/gallito/search_montevideo_sale_apartment.json")
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) { return rpcResponse(body), nil })
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	progress := &Progress{}
	hooks := 0
	source.ProgressHook = func(value *Progress) error {
		hooks++
		if value.Jobs == nil {
			t.Fatal("missing jobs progress")
		}
		return nil
	}
	listings, stats, err := source.CollectJobs(context.Background(), []SearchJob{
		{Region: "Montevideo", City: "Montevideo", Operation: "sale", PropertyType: "apartment"},
		{Region: "Pocitos", City: "Montevideo", Operation: "sale", PropertyType: "apartment"},
	}, 20, 1, progress)
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 10 || len(stats) != 2 || stats[1].Duplicates != 10 {
		t.Fatalf("listings=%d stats=%#v", len(listings), stats)
	}
	if hooks != 2 || len(progress.SeenIDs) != 10 {
		t.Fatalf("hooks=%d seen=%d", hooks, len(progress.SeenIDs))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func rpcResponse(structured []byte) *http.Response {
	body := append([]byte(`{"jsonrpc":"2.0","id":1,"result":{"structuredContent":`), structured...)
	body = append(body, []byte(`,"content":[{"type":"text","text":""}]}}`)...)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}
}
