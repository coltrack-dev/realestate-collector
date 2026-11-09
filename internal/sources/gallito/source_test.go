package gallito

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func TestCollectJobsUsesIndependentRegionalSearches(t *testing.T) {
	responses := map[string][]byte{
		"Montevideo":     []byte(`{"searchId":"search-montevideo","page":1,"totalPages":1,"total":1,"listings":[{"id":"m1","title":"Montevideo","operation":"sale","propertyType":"apartment","city":"MONTEVIDEO","url":"https://gallito.test/m1"}]}`),
		"Punta del Este": []byte(`{"searchId":"search-punta","page":1,"totalPages":1,"total":1,"listings":[{"id":"p1","title":"Punta","operation":"sale","propertyType":"apartment","city":"PUNTA DEL ESTE","url":"https://gallito.test/p1"}]}`),
	}
	var calls []struct {
		method string
		city   string
		search string
	}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var call struct {
			Params struct {
				Name      string `json:"name"`
				Arguments struct {
					City     string `json:"city"`
					SearchID string `json:"searchId"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, struct {
			method string
			city   string
			search string
		}{call.Params.Name, call.Params.Arguments.City, call.Params.Arguments.SearchID})
		if call.Params.Name != "search_listings" {
			t.Fatalf("unexpected method %s", call.Params.Name)
		}
		return rpcResponse(responses[call.Params.Arguments.City]), nil
	})
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	listings, stats, err := source.CollectJobs(context.Background(), []SearchJob{
		{Region: "Montevideo", City: "Montevideo", Operation: "sale", PropertyType: "apartment"},
		{Region: "Punta del Este", City: "Punta del Este", Department: "Maldonado", Operation: "sale", PropertyType: "apartment"},
	}, 2, 1, &Progress{})
	if err != nil || len(listings) != 2 || len(stats) != 2 {
		t.Fatalf("listings=%d stats=%d err=%v", len(listings), len(stats), err)
	}
	if len(calls) != 2 || calls[0].city != "Montevideo" || calls[1].city != "Punta del Este" {
		t.Fatalf("calls=%#v", calls)
	}
	if stats[0].SearchID == stats[1].SearchID || stats[0].ActualCities[0] == stats[1].ActualCities[0] {
		t.Fatalf("regional searches were not independent: %#v", stats)
	}
}

func TestCollectJobsReportsJobsSkippedByGlobalLimit(t *testing.T) {
	body := []byte(`{"searchId":"search-one","page":1,"totalPages":1,"total":1,"listings":[{"id":"a1","title":"A","operation":"sale","propertyType":"apartment","city":"CIUDAD DE LA COSTA","url":"https://gallito.test/a"}]}`)
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) { return rpcResponse(body), nil })
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	_, stats, err := source.CollectJobs(context.Background(), []SearchJob{
		{Region: "Ciudad de la Costa", City: "Ciudad de la Costa", Department: "Canelones", Operation: "sale", PropertyType: "apartment"},
		{Region: "Montevideo", City: "Montevideo", Operation: "sale", PropertyType: "apartment"},
	}, 1, 1, &Progress{})
	if err != nil || len(stats) != 2 {
		t.Fatalf("stats=%#v err=%v", stats, err)
	}
	if stats[1].TerminationReason != "global_limit_reached_before_job" {
		t.Fatalf("termination=%q", stats[1].TerminationReason)
	}
}

func TestProgressSignatureChangesWithSearchFilters(t *testing.T) {
	first := ProgressSignature("UY", "sale", "apartment", "Montevideo", "100", "10")
	second := ProgressSignature("UY", "sale", "apartment", "Punta del Este", "100", "10")
	if first == second || len(first) != 64 {
		t.Fatalf("unexpected signatures: %q %q", first, second)
	}
}

func TestCollectJobsRoundRobinDoesNotExhaustFirstJob(t *testing.T) {
	responses := map[string]map[int][]byte{
		"Montevideo": {
			1: []byte(`{"searchId":"m-search","page":1,"totalPages":2,"listings":[{"id":"m1","title":"M1","city":"MONTEVIDEO","url":"https://gallito.test/m1"}]}`),
			2: []byte(`{"searchId":"m-search","page":2,"totalPages":2,"listings":[{"id":"m2","title":"M2","city":"MONTEVIDEO","url":"https://gallito.test/m2"}]}`),
		},
		"Punta del Este": {
			1: []byte(`{"searchId":"p-search","page":1,"totalPages":2,"listings":[{"id":"p1","title":"P1","city":"PUNTA DEL ESTE","url":"https://gallito.test/p1"}]}`),
			2: []byte(`{"searchId":"p-search","page":2,"totalPages":2,"listings":[{"id":"p2","title":"P2","city":"PUNTA DEL ESTE","url":"https://gallito.test/p2"}]}`),
		},
	}
	var calls []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var call struct {
			Params struct {
				Name      string `json:"name"`
				Arguments struct {
					City     string `json:"city"`
					SearchID string `json:"searchId"`
					Page     int    `json:"page"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		city := call.Params.Arguments.City
		page := call.Params.Arguments.Page
		if call.Params.Name == "search_listings" {
			page = 1
		}
		calls = append(calls, city+":"+call.Params.Name+":"+fmt.Sprint(page))
		var body []byte
		if city == "" {
			if call.Params.Arguments.SearchID == "m-search" {
				city = "Montevideo"
			} else {
				city = "Punta del Este"
			}
		}
		body = responses[city][page]
		return rpcResponse(body), nil
	})
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	listings, stats, err := source.CollectJobs(context.Background(), []SearchJob{
		{Region: "Montevideo", City: "Montevideo", Operation: "sale", PropertyType: "apartment"},
		{Region: "Punta del Este", City: "Punta del Este", Department: "Maldonado", Operation: "sale", PropertyType: "apartment"},
	}, 4, 2, &Progress{})
	if err != nil || len(listings) != 4 || len(stats) != 2 {
		t.Fatalf("listings=%d stats=%d err=%v", len(listings), len(stats), err)
	}
	for index, expected := range []string{"m1", "p1", "m2", "p2"} {
		if listings[index].ExternalID != expected {
			t.Fatalf("round-robin listing[%d]=%q, want %q; calls=%v", index, listings[index].ExternalID, expected, calls)
		}
	}
	if stats[0].PagesFetched != 2 || stats[1].PagesFetched != 2 {
		t.Fatalf("stats=%#v", stats)
	}
}

func TestCollectJobsEmptyJobDoesNotBlockOtherJobs(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var call struct {
			Params struct {
				Arguments struct {
					City string `json:"city"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Arguments.City == "Empty" {
			return rpcResponse([]byte(`{"searchId":"empty","page":1,"totalPages":1,"listings":[]}`)), nil
		}
		return rpcResponse([]byte(`{"searchId":"full","page":1,"totalPages":1,"listings":[{"id":"full-1","title":"Full","city":"MONTEVIDEO","url":"https://gallito.test/full-1"}]}`)), nil
	})
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	listings, stats, err := source.CollectJobs(context.Background(), []SearchJob{
		{Region: "empty", City: "Empty", Operation: "sale", PropertyType: "apartment"},
		{Region: "full", City: "Montevideo", Operation: "sale", PropertyType: "apartment"},
	}, 1, 1, &Progress{})
	if err != nil || len(listings) != 1 || len(stats) != 2 {
		t.Fatalf("listings=%d stats=%d err=%v", len(listings), len(stats), err)
	}
	if stats[0].ListingsFound != 0 || stats[1].ListingsAccepted != 1 {
		t.Fatalf("stats=%#v", stats)
	}
}

func TestCollectJobsResumeKeepsEachJobCursor(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var call struct {
			Params struct {
				Name      string `json:"name"`
				Arguments struct {
					SearchID string `json:"searchId"`
					Page     int    `json:"page"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Name != "more_results" || call.Params.Arguments.Page != 2 {
			t.Fatalf("unexpected resume call: %+v", call)
		}
		id := "m2"
		if call.Params.Arguments.SearchID == "p-search" {
			id = "p2"
		}
		return rpcResponse([]byte(fmt.Sprintf(`{"searchId":%q,"page":2,"totalPages":2,"listings":[{"id":%q,"title":"resumed","url":"https://gallito.test/%s"}]}`, call.Params.Arguments.SearchID, id, id))), nil
	})
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	progress := &Progress{Jobs: map[string]JobProgress{
		jobKey(SearchJob{Region: "Montevideo", City: "Montevideo", Operation: "sale", PropertyType: "apartment"}): {SearchID: "m-search", Page: 1},
		jobKey(SearchJob{Region: "Punta", City: "Punta", Operation: "sale", PropertyType: "apartment"}):           {SearchID: "p-search", Page: 1},
	}, SeenIDs: map[string]bool{}}
	listings, _, err := source.CollectJobs(context.Background(), []SearchJob{
		{Region: "Montevideo", City: "Montevideo", Operation: "sale", PropertyType: "apartment"},
		{Region: "Punta", City: "Punta", Operation: "sale", PropertyType: "apartment"},
	}, 2, 2, progress)
	if err != nil || len(listings) != 2 {
		t.Fatalf("listings=%d err=%v", len(listings), err)
	}
	if progress.Jobs[jobKey(SearchJob{Region: "Montevideo", City: "Montevideo", Operation: "sale", PropertyType: "apartment"})].Page != 2 {
		t.Fatalf("Montevideo cursor was not advanced: %#v", progress.Jobs)
	}
}

func TestCollectJobsDuplicatesDoNotConsumeGlobalLimit(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var call struct {
			Params struct {
				Arguments struct {
					City string `json:"city"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Arguments.City == "First" {
			return rpcResponse([]byte(`{"searchId":"first","page":1,"totalPages":1,"listings":[{"id":"same","title":"Same","url":"https://gallito.test/same"}]}`)), nil
		}
		return rpcResponse([]byte(`{"searchId":"second","page":1,"totalPages":1,"listings":[{"id":"same","title":"Same","url":"https://gallito.test/same"},{"id":"new","title":"New","url":"https://gallito.test/new"}]}`)), nil
	})
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	listings, stats, err := source.CollectJobs(context.Background(), []SearchJob{
		{Region: "first", City: "First", Operation: "sale", PropertyType: "apartment"},
		{Region: "second", City: "Second", Operation: "sale", PropertyType: "apartment"},
	}, 2, 1, &Progress{})
	if err != nil || len(listings) != 2 || stats[1].Duplicates != 1 {
		t.Fatalf("listings=%d stats=%#v err=%v", len(listings), stats, err)
	}
}

func TestCollectJobsExpiredSearchIDRestartsWithoutDuplicate(t *testing.T) {
	callCount := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		callCount++
		var call struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Name == "more_results" {
			return rpcErrorResponse(-32000, "searchId expired"), nil
		}
		return rpcResponse([]byte(`{"searchId":"fresh-search","page":1,"totalPages":1,"listings":[{"id":"new-after-expiry","title":"Fresh","url":"https://gallito.test/fresh"}]}`)), nil
	})
	source := Source{HTTP: &http.Client{Transport: transport}, Endpoint: "https://fixture.invalid/mcp", UserAgent: "test"}
	progress := &Progress{Jobs: map[string]JobProgress{
		jobKey(SearchJob{Region: "Montevideo", City: "Montevideo", Operation: "sale", PropertyType: "apartment"}): {SearchID: "expired-search", Page: 1},
	}, SeenIDs: map[string]bool{"old": true}}
	listings, _, err := source.CollectJobs(context.Background(), []SearchJob{{Region: "Montevideo", City: "Montevideo", Operation: "sale", PropertyType: "apartment"}}, 1, 2, progress)
	if err != nil || len(listings) != 1 || listings[0].ExternalID != "new-after-expiry" || callCount != 2 {
		t.Fatalf("listings=%#v calls=%d err=%v", listings, callCount, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func rpcResponse(structured []byte) *http.Response {
	body := append([]byte(`{"jsonrpc":"2.0","id":1,"result":{"structuredContent":`), structured...)
	body = append(body, []byte(`,"content":[{"type":"text","text":""}]}}`)...)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}
}

func rpcErrorResponse(code int, message string) *http.Response {
	body := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"error":{"code":%d,"message":%q}}`, code, message))
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}
}
