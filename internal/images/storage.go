package images

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Mode string

const (
	ExternalURL  Mode = "EXTERNAL_URL"
	TempCache    Mode = "TEMP_CACHE"
	LocalStorage Mode = "LOCAL_STORAGE"
)

type Asset struct {
	OriginalURL string
	LocalPath   string
	SHA256      string
	MIMEType    string
	Bytes       int64
	FetchedAt   time.Time
	ExpiresAt   *time.Time
}

type Manager struct {
	Client    *http.Client
	Directory string
	MaxBytes  int64
	TTL       time.Duration
	Delay     time.Duration
}

// CleanupExpired removes only files in the explicitly configured cache directory.
// It is intended for TEMP_CACHE and never touches source URLs or PostgreSQL rows.
func CleanupExpired(ctx context.Context, directory string, now time.Time, ttl time.Duration) (int, error) {
	if directory == "" || ttl <= 0 {
		return 0, fmt.Errorf("cache directory and positive TTL are required")
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read image cache: %w", err)
	}
	cutoff := now.Add(-ttl)
	removed := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return removed, fmt.Errorf("stat cached image %s: %w", entry.Name(), err)
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
				return removed, fmt.Errorf("remove expired image %s: %w", entry.Name(), err)
			}
			removed++
		}
	}
	return removed, nil
}

func (m Manager) Resolve(ctx context.Context, urls []string, mode Mode) ([]Asset, error) {
	if mode != ExternalURL && mode != TempCache && mode != LocalStorage {
		return nil, fmt.Errorf("unsupported image storage mode %q", mode)
	}
	assets := make([]Asset, 0, len(urls))
	for _, url := range urls {
		url = strings.TrimSpace(url)
		if url == "" {
			continue
		}
		if mode == ExternalURL {
			assets = append(assets, Asset{OriginalURL: url})
			continue
		}
		asset, err := m.fetch(ctx, url, mode)
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func (m Manager) fetch(ctx context.Context, originalURL string, mode Mode) (Asset, error) {
	if m.Directory == "" {
		return Asset{}, fmt.Errorf("image storage directory is required for %s", mode)
	}
	if m.MaxBytes <= 0 {
		return Asset{}, fmt.Errorf("image max size must be positive")
	}
	if m.Client == nil {
		m.Client = &http.Client{Timeout: 20 * time.Second}
	}
	if m.Delay > 0 {
		timer := time.NewTimer(m.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return Asset{}, ctx.Err()
		case <-timer.C:
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, originalURL, nil)
	if err != nil {
		return Asset{}, fmt.Errorf("create image request: %w", err)
	}
	response, err := m.Client.Do(req)
	if err != nil {
		return Asset{}, fmt.Errorf("download image: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Asset{}, fmt.Errorf("download image: HTTP %s", response.Status)
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(contentType, "image/") {
		return Asset{}, fmt.Errorf("download image: unsupported MIME type %q", response.Header.Get("Content-Type"))
	}
	if response.ContentLength > m.MaxBytes {
		return Asset{}, fmt.Errorf("download image: content length %d exceeds limit %d", response.ContentLength, m.MaxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, m.MaxBytes+1))
	if err != nil {
		return Asset{}, fmt.Errorf("read image: %w", err)
	}
	if int64(len(body)) > m.MaxBytes {
		return Asset{}, fmt.Errorf("download image: response exceeds limit %d", m.MaxBytes)
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	extension := extensionFor(contentType)
	if err := os.MkdirAll(m.Directory, 0o750); err != nil {
		return Asset{}, fmt.Errorf("create image directory: %w", err)
	}
	path := filepath.Join(m.Directory, hash+extension)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		temporary, err := os.CreateTemp(m.Directory, ".image-*")
		if err != nil {
			return Asset{}, fmt.Errorf("create temporary image: %w", err)
		}
		temporaryName := temporary.Name()
		defer os.Remove(temporaryName)
		if _, err = temporary.Write(body); err != nil {
			temporary.Close()
			return Asset{}, fmt.Errorf("write image: %w", err)
		}
		if err = temporary.Close(); err != nil {
			return Asset{}, fmt.Errorf("close temporary image: %w", err)
		}
		if err = os.Rename(temporaryName, path); err != nil && !os.IsExist(err) {
			return Asset{}, fmt.Errorf("store image: %w", err)
		}
	} else if err != nil {
		return Asset{}, fmt.Errorf("check stored image: %w", err)
	}
	now := time.Now().UTC()
	asset := Asset{OriginalURL: originalURL, LocalPath: path, SHA256: hash, MIMEType: contentType, Bytes: int64(len(body)), FetchedAt: now}
	if mode == TempCache {
		expires := now.Add(m.TTL)
		if m.TTL <= 0 {
			expires = now.Add(24 * time.Hour)
		}
		asset.ExpiresAt = &expires
	}
	return asset, nil
}

func extensionFor(contentType string) string {
	extensions, _ := mime.ExtensionsByType(contentType)
	if len(extensions) > 0 {
		return extensions[0]
	}
	return ".img"
}
