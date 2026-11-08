package ciencuadras

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/example/realestate-collector/internal/model"
)

type Source struct {
	Client    *http.Client
	UserAgent string
	BaseURL   string
	Delay     time.Duration
	Live      bool
	AuthMode  string
}

func (s Source) Name() string { return "ciencuadras" }
func (s Source) Collect(ctx context.Context, limit int) ([]model.Listing, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("check Ciencuadras collection context: %w", err)
	}
	if s.AuthMode == "session" {
		return nil, fmt.Errorf("session auth is not enabled for Ciencuadras: no permission for automated access")
	}
	if !s.Live {
		return nil, fmt.Errorf("live Ciencuadras collection disabled: site terms prohibit robots, spiders and scraping; use fixture mode or obtain written permission")
	}
	return nil, fmt.Errorf("live adapter intentionally not enabled")
}
