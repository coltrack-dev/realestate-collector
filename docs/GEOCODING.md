# Geocoding and map fallback

The collector keeps source coordinates and resolved fallback coordinates
separate. Source values always have priority and are never rewritten by a
geocoder.

## Precision order

`EXACT` source coordinates, cached `ADDRESS`/`NEIGHBORHOOD` coordinates, local
`CITY`, `DEPARTMENT`, or `COUNTRY` center, then `UNKNOWN`.

The local implementation is `internal/geocode.LocalDirectory`. It is a small
curated directory for Colombia and Uruguay and is intentionally not a public
mass geocoding service. An external provider can implement the
`geocode.Geocoder` interface later, subject to its terms, rate limits and
storage policy.

Migration `007_listing_geocoding.sql` is additive and must be applied manually.
It stores `resolved_latitude`, `resolved_longitude`, `location_precision`,
`geocode_provider` and `geocoded_at`. Source upserts invalidate those fields
when country, department, city, neighbourhood or address changes.

The frontend uses exact source coordinates for property markers. Fallback
coordinates are used only for camera navigation and are labelled as approximate,
city-only, country-level or unavailable. No `(0,0)` value is used.
