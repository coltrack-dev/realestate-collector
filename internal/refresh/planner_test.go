package refresh

import (
	"sync"
	"testing"
)

func TestRequestViewportPrioritizesCenterAndLimitsSourceQueries(t *testing.T) {
	var calls int
	var mu sync.Mutex
	c := NewCoordinator(&testSaver{}, []Registration{{Name: "city-source", Capabilities: Capabilities{CountryCode: "CO", SupportsCity: true, SupportsOperation: true, SupportsPropertyType: true, SupportsUnfilteredSearch: true, AllowsAutomatedRefresh: true}, Factory: func(Request) Source {
		return testSource{name: "city-source", calls: &calls, mu: &mu, complete: false}
	}}})
	defer c.Close()
	c.clientInterval = 0
	base := testRequest()
	plan := c.RequestViewport(base, []ViewportArea{
		{CountryCode: "CO", Municipality: "Cali", OverlapRatio: .9, OverlapArea: 9},
		{CountryCode: "CO", Municipality: "Bogotá", OverlapRatio: .2, OverlapArea: 2},
		{CountryCode: "CO", Municipality: "Medellín", OverlapRatio: .8, OverlapArea: 8},
	}, "Medellín", 1, "planner-client")
	if len(plan.Queries) != 1 || plan.Queries[0].Area.Municipality != "Medellín" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if len(plan.Skipped) != 2 || plan.Skipped[0].Reason != "max_source_queries_per_viewport" {
		t.Fatalf("unexpected skipped areas: %+v", plan.Skipped)
	}
	waitForStatus(t, c, plan.Queries[0].Result.RefreshID, StatusPartial)
	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("source calls=%d, want 1", got)
	}

	plan = c.RequestViewport(base, []ViewportArea{{CountryCode: "CO", Municipality: "Medellín", OverlapRatio: 1}}, "Medellín", 1, "planner-client-2")
	if len(plan.Queries) != 0 || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "no_supported_source_or_recently_checked" {
		t.Fatalf("cache was not respected: %+v", plan)
	}
}
