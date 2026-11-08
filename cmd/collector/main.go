package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/realestate-collector/internal/config"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/parser"
	"github.com/example/realestate-collector/internal/sources/ciencuadras"
	"github.com/example/realestate-collector/internal/sources/igac"
	"github.com/example/realestate-collector/internal/storage/postgres"
)

type commandOptions struct {
	source       string
	city         string
	divipola     string
	afterPK      string
	yearFrom     string
	yearTo       string
	legalActCode string
	legalActName string
	withValue    bool
	batchSize    int
	limit        int
	dryRun       bool
}

type collectedData struct {
	listings     []model.Listing
	transactions []model.PropertyTransaction
}

func main() {
	options := parseOptions()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if options.source == "igac-transactions" && !options.dryRun {
		if err := runIGACImport(ctx, options); err != nil {
			log.Fatal(err)
		}
		return
	}

	data, err := collect(ctx, options)
	if err != nil {
		log.Fatal(err)
	}

	if options.dryRun {
		if err := printDryRun(data, options.source); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := persist(ctx, data, options.source); err != nil {
		log.Fatal(err)
	}
}

func parseOptions() commandOptions {
	options := commandOptions{}
	flag.StringVar(&options.source, "source", "fixture", "source: fixture, ciencuadras, or igac-transactions")
	flag.IntVar(&options.limit, "limit", 50, "maximum number of records")
	flag.BoolVar(&options.dryRun, "dry-run", false, "parse without database writes")
	flag.StringVar(&options.city, "city", "bogota", "city filter for live sources")
	flag.StringVar(&options.divipola, "divipola", "11001", "DIVIPOLA filter for live sources")
	flag.StringVar(&options.afterPK, "after-pk", "", "resume after this IGAC pk")
	flag.StringVar(&options.yearFrom, "year-from", "", "minimum IGAC filing year")
	flag.StringVar(&options.yearTo, "year-to", "", "maximum IGAC filing year")
	flag.StringVar(&options.legalActCode, "legal-act-code", "", "IGAC legal act code")
	flag.StringVar(&options.legalActName, "legal-act-name", "", "IGAC legal act name")
	flag.BoolVar(&options.withValue, "with-value", false, "only records with non-NULL valor")
	flag.IntVar(&options.batchSize, "batch-size", 100, "records per import batch")
	flag.String("operation", "sale", "operation filter for listing sources")
	flag.String("property", "apartment", "property filter for listing sources")
	flag.Parse()
	return options
}

func collect(ctx context.Context, options commandOptions) (collectedData, error) {
	if options.limit <= 0 {
		return collectedData{}, fmt.Errorf("limit must be greater than zero")
	}
	if err := validateIGACOptions(options); err != nil {
		return collectedData{}, err
	}
	switch options.source {
	case "fixture":
		listings, err := collectFixture()
		if len(listings) > options.limit {
			listings = listings[:options.limit]
		}
		return collectedData{listings: listings}, err
	case "ciencuadras":
		listings, err := (ciencuadras.Source{Live: false}).Collect(ctx, options.limit)
		return collectedData{listings: listings}, err
	case "igac-transactions":
		settings := config.Load()
		batchSize := options.batchSize
		if batchSize <= 0 {
			return collectedData{}, fmt.Errorf("batch-size must be greater than zero")
		}
		transactions, err := (igac.TransactionSource{
			UserAgent:    settings.UserAgent,
			Delay:        settings.Delay,
			PageSize:     batchSize,
			Municipality: options.city,
			Divipola:     options.divipola,
			AfterPK:      options.afterPK,
			YearFrom:     options.yearFrom,
			YearTo:       options.yearTo,
			LegalActCode: options.legalActCode,
			LegalActName: options.legalActName,
			WithValue:    options.withValue,
		}).Collect(ctx, options.limit)
		return collectedData{transactions: transactions}, err
	default:
		return collectedData{}, fmt.Errorf("unknown source %q", options.source)
	}
}

func runIGACImport(ctx context.Context, options commandOptions) error {
	if options.batchSize <= 0 {
		return fmt.Errorf("batch-size must be greater than zero")
	}
	if err := validateIGACOptions(options); err != nil {
		return err
	}
	settings := config.Load()
	if settings.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required for import")
	}
	store, err := postgres.Open(ctx, settings.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	source := igac.TransactionSource{
		UserAgent:    settings.UserAgent,
		Delay:        settings.Delay,
		Municipality: options.city,
		Divipola:     options.divipola,
		YearFrom:     options.yearFrom,
		YearTo:       options.yearTo,
		LegalActCode: options.legalActCode,
		LegalActName: options.legalActName,
		WithValue:    options.withValue,
	}
	cursor := options.afterPK
	remaining := options.limit
	started := time.Now()
	stats := importStats{}
	for remaining > 0 {
		batchLimit := options.batchSize
		if remaining < batchLimit {
			batchLimit = remaining
		}
		batch, fetchErr := source.CollectBatch(ctx, batchLimit, cursor)
		if fetchErr != nil {
			return fmt.Errorf("fetch IGAC batch after pk %q: %w", cursor, fetchErr)
		}
		if len(batch) == 0 {
			break
		}
		for _, transaction := range batch {
			stats.read++
			if transaction.ExternalID == "" {
				stats.skipped++
				continue
			}
			created, saveErr := store.SaveTransaction(ctx, transaction)
			if saveErr != nil {
				return fmt.Errorf("save IGAC transaction %s: %w", transaction.ExternalID, saveErr)
			}
			if created {
				stats.saved++
			} else {
				stats.updated++
			}
		}
		// Advance only after every record in this batch was persisted successfully.
		cursor = batch[len(batch)-1].ExternalID
		remaining -= len(batch)
		stats.lastPK = cursor
		logImportProgress(stats, started)
		if len(batch) < batchLimit {
			break
		}
	}
	logImportProgress(stats, started)
	return nil
}

func validateIGACOptions(options commandOptions) error {
	if options.legalActCode != "" && options.legalActName != "" {
		return fmt.Errorf("use either legal-act-code or legal-act-name, not both")
	}
	if options.yearFrom != "" && options.yearTo != "" && options.yearFrom > options.yearTo {
		return fmt.Errorf("year-from must not be greater than year-to")
	}
	return nil
}

type importStats struct {
	read    int
	saved   int
	updated int
	skipped int
	lastPK  string
}

func logImportProgress(stats importStats, started time.Time) {
	elapsed := time.Since(started).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	slog.Info("IGAC import progress",
		"read", stats.read,
		"saved", stats.saved,
		"updated", stats.updated,
		"skipped", stats.skipped,
		"last_pk", stats.lastPK,
		"records_per_second", float64(stats.read)/elapsed,
	)
}

func collectFixture() ([]model.Listing, error) {
	file, err := os.Open("testdata/fixtures/listings.json")
	if err != nil {
		return nil, fmt.Errorf("open fixture: %w", err)
	}
	defer file.Close()

	listings, err := parser.ParseJSON(file, "fixture")
	if err != nil {
		return nil, fmt.Errorf("parse fixture: %w", err)
	}
	for i := range listings {
		if listings[i].CollectedAt.IsZero() {
			listings[i].CollectedAt = time.Now().UTC()
		}
		if err := parser.Validate(listings[i]); err != nil {
			slog.Error("invalid fixture listing", "id", listings[i].ExternalID, "error", err)
			listings[i].ExternalID = ""
		}
	}
	return listings, nil
}

func printDryRun(data collectedData, source string) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if len(data.transactions) > 0 {
		if err := encoder.Encode(data.transactions); err != nil {
			return fmt.Errorf("encode transaction dry-run: %w", err)
		}
		fmt.Printf("processed=%d dry_run=true source=%s\n", len(data.transactions), source)
		return nil
	}
	if err := encoder.Encode(data.listings); err != nil {
		return fmt.Errorf("encode listing dry-run: %w", err)
	}
	fmt.Printf("processed=%d dry_run=true\n", len(data.listings))
	return nil
}

func persist(ctx context.Context, data collectedData, source string) error {
	settings := config.Load()
	if settings.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required unless -dry-run is used")
	}
	store, err := postgres.Open(ctx, settings.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	if len(data.transactions) > 0 {
		return persistTransactions(ctx, store, data.transactions, source)
	}
	return persistListings(ctx, store, data.listings, source)
}

func persistTransactions(ctx context.Context, store *postgres.Store, transactions []model.PropertyTransaction, source string) error {
	created := 0
	for _, transaction := range transactions {
		wasCreated, err := store.SaveTransaction(ctx, transaction)
		if err != nil {
			return err
		}
		if wasCreated {
			created++
		}
	}
	slog.Info("transaction import complete", "source", source, "records_found", len(transactions), "records_created", created)
	return nil
}

func persistListings(ctx context.Context, store *postgres.Store, listings []model.Listing, source string) error {
	runID, err := store.BeginRun(ctx, source, time.Now().UTC())
	if err != nil {
		return err
	}
	created, snapshots := 0, 0
	for _, listing := range listings {
		if listing.ExternalID == "" {
			continue
		}
		wasCreated, snapshotCreated, saveErr := store.Save(ctx, listing)
		if saveErr != nil {
			_ = store.FinishRun(ctx, runID, "FAILED", len(listings), created, snapshots, saveErr)
			return saveErr
		}
		if wasCreated {
			created++
		}
		if snapshotCreated {
			snapshots++
		}
	}
	if err := store.FinishRun(ctx, runID, "SUCCEEDED", len(listings), created, snapshots, nil); err != nil {
		return err
	}
	slog.Info("collection complete", "source", source, "listings_found", len(listings), "created", created, "snapshots_created", snapshots)
	return nil
}
