package refresh

import (
	"sort"
	"strings"
	"time"
)

// ViewportArea is the geographic result for one municipality intersecting a
// map viewport. Overlap is already calculated by the geographic resolver.
type ViewportArea struct {
	Level          string
	ID             string
	CountryCode    string
	Department     string
	DepartmentID   string
	Municipality   string
	MunicipalityID string
	Locality       string
	LocalityID     string
	DIVIPOLA       string
	OverlapArea    float64
	OverlapRatio   float64
}

type ViewportQuery struct {
	Area   ViewportArea `json:"area"`
	Result Result       `json:"result"`
	Reason string       `json:"reason,omitempty"`
}

type ViewportPlan struct {
	Queries []ViewportQuery `json:"queries"`
	Skipped []ViewportQuery `json:"skipped"`
}

// RequestViewport plans at most maxNewQueries source-level queries. Areas are
// ordered by the municipality at the viewport center and then by overlap.
// A city-only source therefore receives one independent request per selected
// municipality, without pretending that a city filter covers the whole bbox.
func (c *Coordinator) RequestViewport(base Request, areas []ViewportArea, centerMunicipality string, maxNewQueries int, clientKey string) ViewportPlan {
	if maxNewQueries <= 0 {
		maxNewQueries = 3
	}
	ordered := append([]ViewportArea(nil), areas...)
	sort.SliceStable(ordered, func(i, j int) bool {
		iCenter := strings.EqualFold(strings.TrimSpace(ordered[i].Municipality), strings.TrimSpace(centerMunicipality))
		jCenter := strings.EqualFold(strings.TrimSpace(ordered[j].Municipality), strings.TrimSpace(centerMunicipality))
		if iCenter != jCenter {
			return iCenter
		}
		if ordered[i].OverlapRatio != ordered[j].OverlapRatio {
			return ordered[i].OverlapRatio > ordered[j].OverlapRatio
		}
		return ordered[i].OverlapArea > ordered[j].OverlapArea
	})

	plan := ViewportPlan{Queries: make([]ViewportQuery, 0), Skipped: make([]ViewportQuery, 0)}
	requests := make([]Request, 0)
	areasForRequests := make([]ViewportArea, 0)
	planned := 0
	for _, area := range ordered {
		req := base
		if req.CountryCode == "" {
			req.CountryCode = area.CountryCode
		}
		req.CountryCode, req.City, req.Department, req.Locality = area.CountryCode, area.Municipality, area.Department, area.Locality
		if area.CountryCode == "" {
			continue
		}
		filterLevel, ok := c.sourceFilterLevel(req, area)
		if !ok {
			plan.Skipped = append(plan.Skipped, ViewportQuery{Area: area, Reason: "no_source_geography_filter"})
			continue
		}
		if filterLevel != "LOCALITY" {
			req.Locality = ""
		}
		if filterLevel != "MUNICIPALITY" {
			req.City = ""
		}
		if filterLevel != "DEPARTMENT" {
			req.Department = ""
		}
		count := c.newSourceQueryCount(req)
		if count == 0 {
			plan.Skipped = append(plan.Skipped, ViewportQuery{Area: area, Reason: "no_supported_source_or_recently_checked"})
			continue
		}
		if planned+count > maxNewQueries {
			plan.Skipped = append(plan.Skipped, ViewportQuery{Area: area, Reason: "max_source_queries_per_viewport"})
			continue
		}
		planned += count
		requests = append(requests, req)
		areasForRequests = append(areasForRequests, area)
	}
	results := c.RequestBatch(requests, clientKey)
	for i, result := range results {
		plan.Queries = append(plan.Queries, ViewportQuery{Area: areasForRequests[i], Result: result})
	}
	return plan
}

// sourceFilterLevel chooses the most precise geographic filter supported by at
// least one enabled source for this area. It intentionally does not treat a
// locality as a municipality or fall back to a nationwide query.
func (c *Coordinator) sourceFilterLevel(request Request, area ViewportArea) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, registration := range c.registrations {
		capability := registration.Capabilities
		if !capability.AllowsAutomatedRefresh || !strings.EqualFold(capability.CountryCode, area.CountryCode) {
			continue
		}
		if !supportsMunicipality(capability, area.Municipality) {
			continue
		}
		if capability.SupportsLocality && area.Locality != "" {
			return "LOCALITY", true
		}
		if capability.SupportsCity && area.Municipality != "" {
			return "MUNICIPALITY", true
		}
		if capability.SupportsDepartment && area.Department != "" {
			return "DEPARTMENT", true
		}
		if capability.SupportsBBox {
			return "BBOX", true
		}
	}
	return "", false
}

func (c *Coordinator) newSourceQueryCount(request Request) int {
	request = normalizeRequest(request)
	if !request.Bounds.Valid() {
		return 0
	}
	now := timeNowUTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, registration := range c.registrations {
		capability := registration.Capabilities
		if !capability.AllowsAutomatedRefresh || !strings.EqualFold(capability.CountryCode, request.CountryCode) || !supportsMunicipality(capability, request.City) {
			continue
		}
		if !capability.SupportsBBox && !capability.SupportsCity && !capability.SupportsDepartment && !capability.SupportsLocality {
			continue
		}
		if request.Operation == "" || request.PropertyType == "" {
			if !capability.SupportsUnfilteredSearch {
				continue
			}
		}
		name := registration.Name
		if name == "" {
			name = registration.Factory(request).Name()
		}
		identity := sourceQueryIdentityFor(request, name, capability)
		if c.sourceQueryCachedLocked(identity, now) {
			continue
		}
		if _, ok := c.queryInFlight[identity]; ok {
			continue
		}
		count++
	}
	return count
}

// Kept as a small seam to make planner tests deterministic without exposing
// coordinator clock state.
var timeNowUTC = func() time.Time { return time.Now().UTC() }
