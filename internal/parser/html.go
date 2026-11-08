package parser

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/normalize"
)

type fixtureItem struct {
	ID, URL, Title, Description, Operation, PropertyType, Price, Currency, Area, City, Neighborhood string
	Bedrooms, Bathrooms, Parking                                                                    int
	Images                                                                                          []string
}

func ParseHTML(r io.Reader, source string) ([]model.Listing, error) {
	d, e := goquery.NewDocumentFromReader(r)
	if e != nil {
		return nil, e
	}
	var out []model.Listing
	d.Find("article[data-listing-id]").Each(func(_ int, s *goquery.Selection) {
		id, _ := s.Attr("data-listing-id")
		href, _ := s.Find("a").First().Attr("href")
		listing := model.Listing{
			Source:       source,
			ExternalID:   id,
			URL:          href,
			Title:        s.Find(".title").Text(),
			Description:  s.Find(".description").Text(),
			Price:        normalize.PriceCOP(s.Find(".price").Text()),
			Currency:     "COP",
			AreaM2:       normalize.Area(s.Find(".area").Text()),
			Bedrooms:     normalize.Number(s.Find(".bedrooms").Text()),
			Bathrooms:    normalize.Number(s.Find(".bathrooms").Text()),
			City:         s.Find(".city").Text(),
			Neighborhood: s.Find(".neighborhood").Text(),
			Operation:    s.Find(".operation").Text(),
			PropertyType: s.Find(".property-type").Text(),
		}
		out = append(out, normalize.CleanListing(listing))
	})
	return out, nil
}
func ParseJSON(r io.Reader, source string) ([]model.Listing, error) {
	var items []fixtureItem
	if e := json.NewDecoder(r).Decode(&items); e != nil {
		return nil, e
	}
	out := make([]model.Listing, 0, len(items))
	for _, x := range items {
		listing := model.Listing{
			Source:       source,
			ExternalID:   x.ID,
			URL:          x.URL,
			Title:        x.Title,
			Description:  x.Description,
			Operation:    x.Operation,
			PropertyType: x.PropertyType,
			Price:        normalize.PriceCOP(x.Price),
			Currency:     x.Currency,
			AreaM2:       normalize.Area(x.Area),
			Bedrooms:     ptr(x.Bedrooms),
			Bathrooms:    ptr(x.Bathrooms),
			ParkingSpots: ptr(x.Parking),
			Country:      "Colombia",
			City:         x.City,
			Neighborhood: x.Neighborhood,
			Images:       x.Images,
			CollectedAt:  time.Now(),
		}
		out = append(out, normalize.CleanListing(listing))
	}
	return out, nil
}
func ptr(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}
func Validate(l model.Listing) error {
	if l.Source == "" || l.ExternalID == "" || l.URL == "" || l.Operation == "" || l.PropertyType == "" {
		return fmt.Errorf("missing required listing field")
	}
	return nil
}

var _ = strings.TrimSpace
