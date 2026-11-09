package httpclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type metadataKey struct{}

type RequestMetadata struct {
	Source      string
	RefreshID   string
	Operation   string
	City        string
	CountryCode string
	Attempt     int
}

func WithMetadata(ctx context.Context, metadata RequestMetadata) context.Context {
	previous := metadataFromContext(ctx)
	if metadata.Source == "" {
		metadata.Source = previous.Source
	}
	if metadata.RefreshID == "" {
		metadata.RefreshID = previous.RefreshID
	}
	if metadata.Operation == "" {
		metadata.Operation = previous.Operation
	}
	if metadata.City == "" {
		metadata.City = previous.City
	}
	if metadata.CountryCode == "" {
		metadata.CountryCode = previous.CountryCode
	}
	if metadata.Attempt < 1 {
		metadata.Attempt = 1
	}
	return context.WithValue(ctx, metadataKey{}, metadata)
}

func metadataFromContext(ctx context.Context) RequestMetadata {
	if value, ok := ctx.Value(metadataKey{}).(RequestMetadata); ok {
		return value
	}
	return RequestMetadata{Attempt: 1}
}

type LoggingTransport struct {
	Base   http.RoundTripper
	Logger *slog.Logger
	Source string
}

func NewLoggingTransport(base http.RoundTripper, logger *slog.Logger, source string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &LoggingTransport{Base: base, Logger: logger, Source: source}
}

func (t *LoggingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	metadata := metadataFromContext(request.Context())
	if metadata.Source == "" {
		metadata.Source = t.Source
	}
	if metadata.Attempt < 1 {
		metadata.Attempt = 1
	}
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = newRequestID()
	}
	started := time.Now()
	fields := []any{"source", metadata.Source, "method", request.Method, "host", request.URL.Host, "path", request.URL.Path, "request_id", requestID, "refresh_id", optional(metadata.RefreshID), "attempt", metadata.Attempt, "operation", optional(metadata.Operation), "city", optional(metadata.City), "country_code", optional(metadata.CountryCode)}
	t.Logger.Debug("external_http_start", fields...)
	response, err := t.Base.RoundTrip(request)
	duration := time.Since(started)
	complete := []any{"source", metadata.Source, "request_id", requestID, "refresh_id", optional(metadata.RefreshID), "attempt", metadata.Attempt, "duration_ms", duration.Milliseconds()}
	if err != nil {
		result := "error"
		if request.Context().Err() != nil {
			if request.Context().Err() == context.DeadlineExceeded {
				result = "timeout"
			} else {
				result = "cancelled"
			}
		}
		complete = append(complete, "result", result)
		t.Logger.Warn("external_http_complete", complete...)
		return nil, err
	}
	complete = append(complete, "status", response.StatusCode, "result", responseResult(response.StatusCode))
	if response.ContentLength >= 0 {
		complete = append(complete, "response_size_bytes", response.ContentLength)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		retryAfter := strings.TrimSpace(response.Header.Get("Retry-After"))
		backoff := retryAfterDuration(retryAfter, metadata.Attempt)
		complete = append(complete, "retry_after", optional(retryAfter), "calculated_backoff_ms", backoff.Milliseconds(), "next_allowed_at", time.Now().Add(backoff).UTC().Format(time.RFC3339Nano))
	}
	if response.StatusCode >= 400 {
		t.Logger.Warn("external_http_complete", complete...)
	} else {
		t.Logger.Info("external_http_complete", complete...)
	}
	return response, nil
}

func responseResult(status int) string {
	if status >= 200 && status < 300 {
		return "success"
	}
	return "error"
}
func optional(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func retryAfterDuration(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(attempt+1) * time.Second
}
func newRequestID() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "external-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return "external-" + hex.EncodeToString(bytes[:])
}
