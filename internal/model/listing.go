package model

import "time"

type Listing struct {
	Source, ExternalID, URL                          string
	Title, Description                               string
	Operation, PropertyType                          string
	Price                                            *int64
	Currency                                         string
	AreaM2                                           *float64
	Bedrooms, Bathrooms, ParkingSpots                *int
	Country, Department, City, Neighborhood, Address string
	Latitude, Longitude                              *float64
	Images                                           []string
	PublishedAt                                      *time.Time
	CollectedAt                                      time.Time
}
