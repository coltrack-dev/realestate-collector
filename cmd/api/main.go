package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/realestate-collector/internal/api"
	"github.com/example/realestate-collector/internal/collector"
	"github.com/example/realestate-collector/internal/config"
	"github.com/example/realestate-collector/internal/geo"
	"github.com/example/realestate-collector/internal/refresh"
	"github.com/example/realestate-collector/internal/sources/dccolombia"
	"github.com/example/realestate-collector/internal/sources/gallito"
	"github.com/example/realestate-collector/internal/sources/vixora"
	"github.com/example/realestate-collector/internal/storage/postgres"
)

func main() {
	settings := config.Load()
	if settings.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := postgres.Open(ctx, settings.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	address := os.Getenv("API_ADDR")
	if address == "" {
		address = "127.0.0.1:8086"
	}
	repository := postgres.NewReadRepository(store)
	details := api.NewDetailsService(repository, []api.DetailSource{
		vixora.New(nil, settings.UserAgent, settings.Delay),
		gallito.New(nil, settings.UserAgent, settings.Delay),
	}, settings.DetailsTTL)
	coordinator := refresh.NewCoordinator(store, []refresh.Registration{
		{
			Name:         "vixora",
			Capabilities: refresh.Capabilities{CountryCode: "CO", SupportsCity: true, SupportsOperation: true, SupportsPropertyType: true, SupportsUnfilteredSearch: true, SupportedMunicipalities: vixora.SupportedMunicipalities(), AllowsAutomatedRefresh: settings.SourceRefreshEnabled, MinRequestInterval: 5 * time.Second, CacheTTL: 5 * time.Minute},
			Factory: func(request refresh.Request) refresh.Source {
				source := vixora.New(nil, settings.UserAgent, settings.Delay)
				source.Operation, source.PropertyType, source.Municipality = request.Operation, request.PropertyType, request.City
				return sourceAdapter{source: source, collect: func(ctx context.Context, limit int) (refresh.CollectResult, error) {
					return source.CollectForRefresh(ctx, limit)
				}}
			},
		},
		{
			Name:         "dccolombia",
			Capabilities: refresh.Capabilities{CountryCode: "CO", SupportsCity: true, SupportsOperation: true, SupportsPropertyType: true, SupportsUnfilteredSearch: true, AllowsAutomatedRefresh: settings.SourceRefreshEnabled, MinRequestInterval: 5 * time.Second, CacheTTL: 5 * time.Minute},
			Factory: func(request refresh.Request) refresh.Source {
				source := dccolombia.New(nil, settings.UserAgent, settings.Delay)
				source.City, source.Operation, source.PropertyType = request.City, request.Operation, request.PropertyType
				return sourceAdapter{source: source, collect: func(ctx context.Context, limit int) (refresh.CollectResult, error) {
					return source.CollectForRefresh(ctx, limit)
				}}
			},
		},
		{
			Name:         "gallito",
			Capabilities: refresh.Capabilities{CountryCode: "UY", SupportsCity: true, SupportsOperation: true, SupportsPropertyType: true, AllowsAutomatedRefresh: false},
		},
	})
	defer coordinator.Close()
	geoResolver, err := geo.NewFromFile(os.Getenv("GEO_BOUNDARIES_PATH"))
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr: address, Handler: api.NewServerWithGeo(repository, details, coordinator, geoResolver),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
	go func() {
		slog.Info("listings API listening", "address", address, "source_refresh_enabled", coordinator.Enabled(), "geo_boundaries_configured", os.Getenv("GEO_BOUNDARIES_PATH") != "")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("API server: %v", err)
		}
	}()
	<-ctx.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("API shutdown: %v", err)
	}
}

type sourceAdapter struct {
	source  collector.Source
	collect func(context.Context, int) (refresh.CollectResult, error)
}

func (s sourceAdapter) Name() string { return s.source.Name() }
func (s sourceAdapter) Collect(ctx context.Context, _ refresh.Request, limit int) (refresh.CollectResult, error) {
	if s.collect != nil {
		return s.collect(ctx, limit)
	}
	listings, err := s.source.Collect(ctx, limit)
	return refresh.CollectResult{Listings: listings, FetchedCount: len(listings), Complete: false, PartialReason: "adapter_completeness_unknown"}, err
}
