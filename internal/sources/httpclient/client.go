package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client is a deliberately small sequential HTTP client for permitted public
// APIs. It never changes proxy settings or attempts to bypass access controls.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	Delay     time.Duration
	Source    string

	mu          sync.Mutex
	lastRequest time.Time
}

func New(httpClient *http.Client, userAgent string, delay time.Duration) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{HTTP: httpClient, UserAgent: userAgent, Delay: delay}
}

func (c *Client) SetSource(source string) {
	c.Source = source
	c.HTTP.Transport = NewLoggingTransport(c.HTTP.Transport, slog.Default(), source)
}

func (c *Client) GetJSON(ctx context.Context, endpoint string, target any) error {
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.wait(ctx); err != nil {
			return err
		}
		requestContext := WithMetadata(ctx, RequestMetadata{Source: c.Source, Attempt: attempt + 1})
		request, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("create GET request: %w", err)
		}
		request.Header.Set("Accept", "application/json")
		if c.UserAgent != "" {
			request.Header.Set("User-Agent", c.UserAgent)
		}
		response, err := c.HTTP.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
				continue
			}
			return fmt.Errorf("GET %s: %w", safeEndpoint(endpoint), err)
		}

		body, readErr := io.ReadAll(io.LimitReader(response.Body, 10<<20))
		response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read %s response: %w", safeEndpoint(endpoint), readErr)
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("GET %s: access denied with HTTP %d; stopping", safeEndpoint(endpoint), response.StatusCode)
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			if attempt < 2 {
				if err := waitRetryAfter(ctx, response.Header.Get("Retry-After"), attempt); err != nil {
					return err
				}
				continue
			}
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("GET %s: HTTP %d", safeEndpoint(endpoint), response.StatusCode)
		}
		if err := json.Unmarshal(body, target); err != nil {
			return fmt.Errorf("decode JSON from %s: %w", safeEndpoint(endpoint), err)
		}
		return nil
	}
	return fmt.Errorf("GET %s: request attempts exhausted", safeEndpoint(endpoint))
}

func safeEndpoint(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return "external endpoint"
	}
	if parsed.Host == "" {
		return parsed.Path
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.Path
}

func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	waitFor := c.Delay - time.Since(c.lastRequest)
	if waitFor < 0 {
		waitFor = 0
	}
	c.lastRequest = time.Now()
	c.mu.Unlock()
	if waitFor == 0 {
		return nil
	}
	timer := time.NewTimer(waitFor)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func waitRetryAfter(ctx context.Context, value string, attempt int) error {
	delay := time.Duration(attempt+1) * time.Second
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		delay = time.Duration(seconds) * time.Second
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
