package images

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExternalURLDoesNotDownload(t *testing.T) {
	manager := Manager{Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected request"); return nil, nil })}}
	assets, err := manager.Resolve(context.Background(), []string{"https://source.test/a.jpg"}, ExternalURL)
	if err != nil || len(assets) != 1 || assets[0].OriginalURL != "https://source.test/a.jpg" || assets[0].LocalPath != "" {
		t.Fatalf("unexpected external asset: %#v, %v", assets, err)
	}
}

func TestLocalStorageValidatesMIMEAndDeduplicates(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/fixtures/images/test.svg")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/svg+xml"}}, Body: io.NopCloser(strings.NewReader(string(fixture)))}, nil
	})}
	manager := Manager{Client: client, Directory: directory, MaxBytes: 4096}
	assets, err := manager.Resolve(context.Background(), []string{"https://source.test/image.svg", "https://source.test/image.svg"}, LocalStorage)
	if err != nil || len(assets) != 2 {
		t.Fatalf("unexpected assets: %#v, %v", assets, err)
	}
	if assets[0].SHA256 == "" || assets[0].SHA256 != assets[1].SHA256 {
		t.Fatalf("hash deduplication failed: %#v", assets)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one stored image, got %d", len(entries))
	}
	if filepath.Ext(assets[0].LocalPath) != ".svg" {
		t.Fatalf("unexpected extension: %s", assets[0].LocalPath)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
