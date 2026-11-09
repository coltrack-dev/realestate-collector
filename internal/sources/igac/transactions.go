package igac

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/sources/httpclient"
)

const DefaultEndpoint = "https://www.datos.gov.co/resource/7y2j-43cv.json"

type TransactionSource struct {
	Client       *http.Client
	Endpoint     string
	UserAgent    string
	PageSize     int
	Delay        time.Duration
	MaxRetries   int
	Municipality string
	Divipola     string
	AfterPK      string
	YearFrom     string
	YearTo       string
	LegalActCode string
	LegalActName string
	WithValue    bool
}

// CollectBatch fetches one ordered page. The caller advances the cursor only
// after successfully persisting the returned batch.
func (s TransactionSource) CollectBatch(ctx context.Context, limit int, afterPK string) ([]model.PropertyTransaction, error) {
	if limit <= 0 {
		return nil, nil
	}
	settings := s.settings()
	pageSource := s
	pageSource.AfterPK = afterPK
	return pageSource.fetchPage(ctx, settings, 0, limit)
}

func (s TransactionSource) Name() string { return "igac-transactions" }

func (s TransactionSource) Collect(ctx context.Context, limit int) ([]model.PropertyTransaction, error) {
	if limit <= 0 {
		return nil, nil
	}
	settings := s.settings()
	var result []model.PropertyTransaction
	for offset := 0; len(result) < limit; offset += settings.pageSize {
		if offset > 0 {
			if err := wait(ctx, settings.delay); err != nil {
				return result, err
			}
		}
		count := settings.pageSize
		if remaining := limit - len(result); remaining < count {
			count = remaining
		}
		values, err := s.fetchPage(ctx, settings, offset, count)
		if err != nil {
			return result, err
		}
		result = append(result, values...)
		if len(values) < count {
			break
		}
	}
	return result, nil
}

type sourceSettings struct {
	client     *http.Client
	endpoint   string
	pageSize   int
	delay      time.Duration
	maxRetries int
}

func (s TransactionSource) settings() sourceSettings {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	client.Transport = httpclient.NewLoggingTransport(client.Transport, nil, "igac")
	pageSize := s.PageSize
	if pageSize <= 0 || pageSize > 50000 {
		pageSize = 500
	}
	delay := s.Delay
	if delay < 0 {
		delay = 0
	}
	maxRetries := s.MaxRetries
	if maxRetries < 0 || maxRetries > 5 {
		maxRetries = 3
	}
	endpoint := s.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return sourceSettings{client: client, endpoint: endpoint, pageSize: pageSize, delay: delay, maxRetries: maxRetries}
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay == 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s TransactionSource) fetchPage(ctx context.Context, settings sourceSettings, offset, limit int) ([]model.PropertyTransaction, error) {
	requestURL, err := s.buildPageURL(settings.endpoint, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("build IGAC page URL: %w", err)
	}
	for attempt := 0; ; attempt++ {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if e != nil {
			return nil, fmt.Errorf("create IGAC request: %w", e)
		}
		req.Header.Set("Accept", "application/json")
		if s.UserAgent != "" {
			req.Header.Set("User-Agent", s.UserAgent)
		}
		resp, e := settings.client.Do(req)
		if e == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			listings, decodeErr := decodePage(resp.Body, s.Name())
			resp.Body.Close()
			if decodeErr != nil {
				return nil, fmt.Errorf("decode IGAC response: %w", decodeErr)
			}
			return listings, nil
		}
		if resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if e == nil && (resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return nil, fmt.Errorf("IGAC API refused request: HTTP %d", resp.StatusCode)
		}
		if attempt >= settings.maxRetries {
			if e != nil {
				return nil, fmt.Errorf("request IGAC API: %w", e)
			}
			return nil, fmt.Errorf("IGAC API HTTP %d", resp.StatusCode)
		}
		backoff := time.Duration(1<<attempt) * time.Second
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			if retry, parseErr := strconv.Atoi(resp.Header.Get("Retry-After")); parseErr == nil && retry > 0 {
				backoff = time.Duration(retry) * time.Second
			}
		}
		if err := wait(ctx, backoff); err != nil {
			return nil, err
		}
	}
}

func (s TransactionSource) buildPageURL(endpoint string, offset, limit int) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("$limit", strconv.Itoa(limit))
	query.Set("$offset", strconv.Itoa(offset))
	query.Set("$order", "pk")
	where := make([]string, 0, 2)
	if divipola := strings.TrimSpace(s.Divipola); divipola != "" {
		where = append(where, fmt.Sprintf("divipola = '%s'", strings.ReplaceAll(divipola, "'", "''")))
	} else if municipality := normalizeMunicipality(s.Municipality); municipality != "" {
		where = append(where, fmt.Sprintf("upper(municipio) = '%s'", strings.ToUpper(strings.ReplaceAll(municipality, "'", "''"))))
	}
	if afterPK := strings.TrimSpace(s.AfterPK); afterPK != "" {
		where = append(where, fmt.Sprintf("pk > '%s'", strings.ReplaceAll(afterPK, "'", "''")))
	}
	if yearFrom := strings.TrimSpace(s.YearFrom); yearFrom != "" {
		where = append(where, fmt.Sprintf("year_radica >= '%s'", strings.ReplaceAll(yearFrom, "'", "''")))
	}
	if yearTo := strings.TrimSpace(s.YearTo); yearTo != "" {
		where = append(where, fmt.Sprintf("year_radica <= '%s'", strings.ReplaceAll(yearTo, "'", "''")))
	}
	if legalActCode := strings.TrimSpace(s.LegalActCode); legalActCode != "" {
		where = append(where, fmt.Sprintf("cod_natujur = '%s'", strings.ReplaceAll(legalActCode, "'", "''")))
	} else if legalActName := strings.TrimSpace(s.LegalActName); legalActName != "" {
		where = append(where, fmt.Sprintf("upper(nombre_natujur) = '%s'", strings.ToUpper(strings.ReplaceAll(legalActName, "'", "''"))))
	}
	if s.WithValue {
		where = append(where, "valor IS NOT NULL")
	}
	if len(where) > 0 {
		query.Set("$where", strings.Join(where, " AND "))
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func normalizeMunicipality(value string) string {
	municipality := strings.TrimSpace(value)
	if strings.EqualFold(municipality, "bogota") || strings.EqualFold(municipality, "bogotá") {
		return "BOGOTA D.C."
	}
	return municipality
}

func decodePage(r io.Reader, source string) ([]model.PropertyTransaction, error) {
	var rows []map[string]any
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return nil, err
	}
	out := make([]model.PropertyTransaction, 0, len(rows))
	for _, row := range rows {
		id := text(row, "pk")
		if id == "" {
			continue
		}
		transaction := model.PropertyTransaction{
			Source:                 source,
			ExternalID:             id,
			Matricula:              text(row, "matricula"),
			OldCadastralNumber:     text(row, "numero_catastral_antiguo"),
			CurrentCadastralNumber: text(row, "numero_catastral"),
			Municipality:           text(row, "municipio"),
			Department:             text(row, "departamento"),
			Divipola:               text(row, "divipola"),
			ORIP:                   text(row, "orip"),
			FilingDateText:         text(row, "fecha_radica_texto"),
			OpeningDateText:        text(row, "fecha_apertura_texto"),
			Year:                   text(row, "year_radica"),
			PropertyZone:           text(row, "tipo_predio_zona"),
			Rurality:               text(row, "categoria_ruralidad_2024"),
			AnnotationNumber:       intText(row, "num_anotacion"),
			FolioStatus:            text(row, "estado_folio"),
			DerivedFolios:          text(row, "folios_derivados"),
			NewProperties:          intPtr(row, "predios_nuevos"),
			HasMultipleValue:       boolPtr(row, "tiene_mas_de_un_valor"),
			Value:                  int64Ptr(row, "valor"),
			HasValue:               boolPtr(row, "tiene_valor"),
			CountA:                 intPtr(row, "count_a"),
			CountDE:                intPtr(row, "count_de"),
			JustifyingDocument:     text(row, "documento_justificativo"),
			LegalActCode:           text(row, "cod_natujur"),
			LegalActName:           text(row, "nombre_natujur"),
			Dynamic:                intPtr(row, "dinamica_2024"),
			CollectedAt:            time.Now().UTC(),
		}
		out = append(out, transaction)
	}
	return out, nil
}
func intText(m map[string]any, key string) string {
	if value := intPtr(m, key); value != nil {
		return strconv.Itoa(*value)
	}
	return ""
}
func text(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		return fmt.Sprint(v)
	}
	return ""
}
func int64Ptr(m map[string]any, key string) *int64 {
	v, ok := m[key]
	if !ok {
		return nil
	}
	var n int64
	switch x := v.(type) {
	case float64:
		n = int64(x)
	case string:
		f, err := strconv.ParseFloat(strings.ReplaceAll(x, ",", "."), 64)
		if err != nil {
			return nil
		}
		n = int64(f)
	default:
		return nil
	}
	return &n
}
func intPtr(m map[string]any, key string) *int {
	v := int64Ptr(m, key)
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}
func boolPtr(m map[string]any, key string) *bool {
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case bool:
		return &x
	case string:
		b := strings.EqualFold(x, "si") || strings.EqualFold(x, "true") || x == "1"
		return &b
	}
	return nil
}
