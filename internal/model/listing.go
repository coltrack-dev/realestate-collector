package model

import "time"

type Listing struct {
	Source            string     `json:"source"`
	ExternalID        string     `json:"external_id"`
	URL               string     `json:"url"`
	Title             string     `json:"title"`
	Description       string     `json:"description"`
	Operation         string     `json:"operation"`
	PropertyType      string     `json:"property_type"`
	Price             *int64     `json:"price"`
	Currency          string     `json:"currency"`
	AreaM2            *float64   `json:"area_m2"`
	Bedrooms          *int       `json:"bedrooms"`
	Bathrooms         *int       `json:"bathrooms"`
	ParkingSpots      *int       `json:"parking_spots"`
	Country           string     `json:"country"`
	CountryCode       string     `json:"country_code"`
	Department        string     `json:"department"`
	City              string     `json:"city"`
	Neighborhood      string     `json:"neighborhood"`
	Address           string     `json:"address"`
	Latitude          *float64   `json:"latitude"`
	Longitude         *float64   `json:"longitude"`
	ResolvedLatitude  *float64   `json:"resolved_latitude"`
	ResolvedLongitude *float64   `json:"resolved_longitude"`
	LocationPrecision string     `json:"location_precision"`
	GeocodeProvider   string     `json:"geocode_provider"`
	GeocodedAt        *time.Time `json:"geocoded_at"`
	Images            []string   `json:"images"`
	PublishedAt       *time.Time `json:"published_at"`
	CollectedAt       time.Time  `json:"collected_at"`
	DetailsFetchedAt  *time.Time `json:"details_fetched_at"`
	DetailsExpiresAt  *time.Time `json:"details_expires_at"`
	DetailsStatus     string     `json:"details_status"`
}
