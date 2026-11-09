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
	"github.com/example/realestate-collector/internal/config"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/refresh"
	"github.com/example/realestate-collector/internal/collector"
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
			Name: "vixora",
			Capabilities: refresh.Capabilities{CountryCode: "CO", SupportsCity: true, SupportsOperation: true, SupportsPropertyType: true, AllowsAutomatedRefresh: settings.SourceRefreshEnabled, MinRequestInterval: 5 * time.Second, CacheTTL: 5 * time.Minute},
			Factory: func(request refresh.Request) refresh.Source {
				source := vixora.New(nil, settings.UserAgent, settings.Delay)
				source.Operation, source.PropertyType, source.Municipality = request.Operation, request.PropertyType, request.City
				return sourceAdapter{source: source}
			},
		},
		{
			Name: "dccolombia",
			Capabilities: refresh.Capabilities{CountryCode: "CO", SupportsCity: true, SupportsOperation: true, SupportsPropertyType: true, AllowsAutomatedRefresh: settings.SourceRefreshEnabled, MinRequestInterval: 5 * time.Second, CacheTTL: 5 * time.Minute},
			Factory: func(request refresh.Request) refresh.Source {
				source := dccolombia.New(nil, settings.UserAgent, settings.Delay)
				source.City, source.Operation, source.PropertyType = request.City, request.Operation, request.PropertyType
				return sourceAdapter{source: source}
			},
		},
		{
			Name: "gallito",
			Capabilities: refresh.Capabilities{CountryCode: "UY", SupportsCity: true, SupportsOperation: true, SupportsPropertyType: true, AllowsAutomatedRefresh: false},
		},
	})
	defer coordinator.Close()
	server := &http.Server{
		Addr: address, Handler: api.NewServerWithRefresh(repository, details, coordinator),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
	go func() {
		slog.Info("listings API listening", "address", address)
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

type sourceAdapter struct { source collector.Source }
func (s sourceAdapter) Name() string { return s.source.Name() }
func (s sourceAdapter) Collect(ctx context.Context, _ refresh.Request, limit int) ([]model.Listing, error) { return s.source.Collect(ctx, limit) }
