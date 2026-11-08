package collector

import (
	"context"
	"github.com/example/realestate-collector/internal/model"
)

type Source interface {
	Name() string
	Collect(context.Context, int) ([]model.Listing, error)
}

type DetailSource interface {
		Name() string
		FetchDetails(context.Context, string) (model.Listing, error)
}
