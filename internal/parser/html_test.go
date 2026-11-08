package parser

import (
	"strings"
	"testing"
)

func TestParseHTML(t *testing.T) {
	x := `<article data-listing-id="a1">
        <a href="https://x/a1">x</a>
        <span class="title">Apartamento</span>
        <span class="price">$ 10.000</span>
        <span class="area">50 m2</span>
        <span class="operation">Venta</span>
        <span class="property-type">Apartamento</span>
        <span class="city">Bogota</span>
    </article>`
	ls, e := ParseHTML(strings.NewReader(x), "fixture")
	if e != nil || len(ls) != 1 || ls[0].ExternalID != "a1" || *ls[0].Price != 10000 {
		t.Fatalf("%v %#v", e, ls)
	}
}
