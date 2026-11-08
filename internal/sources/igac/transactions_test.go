package igac

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDecodePage(t *testing.T) {
	data := `[{"pk":"abc","matricula":"050-1","numero_catastral_antiguo":"old","numero_catastral":"new","municipio":"Bogotá D.C.","departamento":"Bogotá D.C.","divipola":"11001","orip":"050","fecha_radica_texto":"2023-01-01","fecha_apertura_texto":"2000-01-01","year_radica":"2023","tipo_predio_zona":"URBANO","categoria_ruralidad_2024":"URBANO","num_anotacion":4,"estado_folio":"ACTIVO","folios_derivados":"[]","predios_nuevos":1,"tiene_mas_de_un_valor":"NO","valor":125000000,"tiene_valor":"SI","count_a":2,"count_de":3,"documento_justificativo":"ESCRITURA","cod_natujur":"125","nombre_natujur":"COMPRAVENTA","dinamica_2024":1}]`
	rows, e := decodePage(strings.NewReader(data), "igac-transactions")
	if e != nil || len(rows) != 1 || rows[0].ExternalID != "abc" || rows[0].LegalActCode != "125" || rows[0].LegalActName != "COMPRAVENTA" || *rows[0].Value != 125000000 || !*rows[0].HasValue || *rows[0].CountDE != 3 {
		t.Fatalf("%v %#v", e, rows)
	}
}

func TestBuildPageURLUsesDivipolaAndCursor(t *testing.T) {
	source := TransactionSource{Divipola: "11001", AfterPK: "abc"}
	pageURL, err := source.buildPageURL("https://example.test/resource.json", 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pageURL, "divipola") || !strings.Contains(pageURL, "11001") || !strings.Contains(pageURL, "pk") || !strings.Contains(pageURL, "abc") {
		t.Fatalf("unexpected URL: %s", pageURL)
	}
}
func TestCollectPaginationAndCancellation(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `[]`
		if calls == 1 {
			body = `[{"pk":"1"}]`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rows, e := (TransactionSource{Client: client, Endpoint: "http://fixture.invalid", PageSize: 1, Delay: time.Millisecond}).Collect(ctx, 2)
	if e != nil || len(rows) != 1 || calls != 2 {
		t.Fatalf("rows=%d calls=%d err=%v", len(rows), calls, e)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
