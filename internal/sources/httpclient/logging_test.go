package httpclient

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestLoggingTransportEmitsSafeRequestLifecycle(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	transport := NewLoggingTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, ContentLength: 12, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}), logger, "vixora")
	request, err := http.NewRequestWithContext(WithMetadata(context.Background(), RequestMetadata{RefreshID: "refresh-1", City: "Bogotá", CountryCode: "CO", Attempt: 2}), http.MethodGet, "https://example.test/properties?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	output := logs.String()
	for _, value := range []string{"external_http_start", "external_http_complete", "vixora", "refresh-1", "attempt", "2"} {
		if !strings.Contains(output, value) {
			t.Fatalf("logs do not contain %q: %s", value, output)
		}
	}
	if strings.Contains(output, "token") || strings.Contains(output, "secret") {
		t.Fatalf("query secret leaked: %s", output)
	}
}

func TestLoggingTransportMarksRateLimit(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	transport := NewLoggingTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, ContentLength: 0, Header: http.Header{"Retry-After": []string{"3"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}), logger, "dccolombia")
	request, _ := http.NewRequest(http.MethodGet, "https://example.test/properties", nil)
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	output := logs.String()
	for _, value := range []string{"retry_after", "3", "calculated_backoff_ms", "next_allowed_at", "\"result\":\"error\""} {
		if !strings.Contains(output, value) {
			t.Fatalf("rate-limit fields missing %q: %s", value, output)
		}
	}
}
