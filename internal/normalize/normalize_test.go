package normalize

import "testing"

func TestPriceCOP(t *testing.T) {
	v := PriceCOP("$ 1.250.000.000 COP")
	if v == nil || *v != 1250000000 {
		t.Fatalf("got %v", v)
	}
}
func TestCityAndURL(t *testing.T) {
	if City("Bogota") != "Bogotá D.C." {
		t.Fatal("city")
	}
	if CanonicalURL("https://x.test/a?utm_source=x&b=1") != "https://x.test/a?b=1" {
		t.Fatal("url")
	}
}
