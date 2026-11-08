package normalize

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/example/realestate-collector/internal/model"
)

var nonDigit = regexp.MustCompile(`[^0-9]`)

func Text(s string) string { return strings.Join(strings.Fields(strings.TrimSpace(s)), " ") }

func PriceCOP(s string) *int64 {
	s = nonDigit.ReplaceAllString(s, "")
	if s == "" {
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

func Number(s string) *int {
	s = nonDigit.ReplaceAllString(s, "")
	if s == "" {
		return nil
	}
	v, e := strconv.Atoi(s)
	if e != nil {
		return nil
	}
	return &v
}
func Area(s string) *float64 {
	s = Text(strings.ReplaceAll(strings.ToLower(s), ",", "."))
	re := regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
	m := re.FindString(s)
	if m == "" {
		return nil
	}
	v, e := strconv.ParseFloat(m, 64)
	if e != nil {
		return nil
	}
	return &v
}

func Operation(s string) string {
	s = strings.ToLower(Text(s))
	if strings.Contains(s, "venta") || strings.Contains(s, "sale") {
		return "SALE"
	}
	if strings.Contains(s, "arriendo") || strings.Contains(s, "rent") {
		return "RENT"
	}
	return ""
}
func PropertyType(s string) string {
	s = strings.ToLower(Text(s))
	if strings.Contains(s, "apart") {
		return "APARTMENT"
	}
	return strings.ToUpper(s)
}
func City(s string) string {
	s = strings.ToLower(Text(s))
	if strings.Contains(s, "bogot") {
		return "Bogotá D.C."
	}
	return Text(s)
}
func CanonicalURL(raw string) string {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil {
		return raw
	}
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(strings.ToLower(k), "utm_") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
func Hashable(l model.Listing) string {
	var b strings.Builder
	fields := []string{
		l.Source, l.ExternalID, l.URL, l.Title, l.Description,
		l.Operation, l.PropertyType, l.Currency, l.Country,
		l.Department, l.City, l.Neighborhood, l.Address,
	}
	for _, f := range fields {
		b.WriteString(Text(f))
		b.WriteByte('|')
	}
	if l.Price != nil {
		b.WriteString(strconv.FormatInt(*l.Price, 10))
	}
	if l.AreaM2 != nil {
		b.WriteString(strconv.FormatFloat(*l.AreaM2, 'f', 2, 64))
	}
	if l.Bedrooms != nil {
		b.WriteString(strconv.Itoa(*l.Bedrooms))
	}
	if l.Bathrooms != nil {
		b.WriteString(strconv.Itoa(*l.Bathrooms))
	}
	if l.ParkingSpots != nil {
		b.WriteString(strconv.Itoa(*l.ParkingSpots))
	}
	return b.String()
}
func CleanListing(l model.Listing) model.Listing {
	l.Title = Text(l.Title)
	l.Description = Text(l.Description)
	l.URL = CanonicalURL(l.URL)
	l.Operation = Operation(l.Operation)
	l.PropertyType = PropertyType(l.PropertyType)
	l.City = City(l.City)
	if l.Country == "" {
		l.Country = "Colombia"
	}
	return l
}

var _ = unicode.IsSpace
