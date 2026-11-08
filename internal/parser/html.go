package parser

import (
	"fmt"
	"io"

	"github.com/PuerkitoBio/goquery"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/normalize"
)

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
	return ParseListingsJSON(r, source)
}

func Validate(l model.Listing) error {
	if l.Source == "" || l.ExternalID == "" || l.URL == "" || l.Operation == "" || l.PropertyType == "" {
		return fmt.Errorf("missing required listing field")
	}
	return nil
}
