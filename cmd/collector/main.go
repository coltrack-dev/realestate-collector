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
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/example/realestate-collector/internal/config"
	"github.com/example/realestate-collector/internal/importer"
	"github.com/example/realestate-collector/internal/model"
	"github.com/example/realestate-collector/internal/parser"
	"github.com/example/realestate-collector/internal/sources/ciencuadras"
	"github.com/example/realestate-collector/internal/sources/dccolombia"
	"github.com/example/realestate-collector/internal/sources/gallito"
	"github.com/example/realestate-collector/internal/sources/igac"
	"github.com/example/realestate-collector/internal/sources/vixora"
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
	input        string
	format       string
	sourceName   string
	operation    string
	property     string
	country      string
	maxListings  int
	maxPages     int
	resume       bool
	region       string
	progressFile string
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
		if len(data.listings) > 0 {
			stats, importErr := importer.Import(ctx, data.listings, nil, true)
			if importErr != nil {
				log.Fatal(importErr)
			}
			slog.Info("listing dry-run complete", "read", stats.Read, "accepted", stats.Accepted,
				"skipped", stats.Skipped, "errors", stats.Errors)
		}
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
	flag.StringVar(&options.source, "source", "fixture", "source: fixture, file, ciencuadras, vixora, dccolombia, gallito, or igac-transactions")
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
	flag.StringVar(&options.input, "input", "", "local JSON or CSV file for -source=file")
	flag.StringVar(&options.format, "format", "", "input format: json or csv; inferred from extension")
	flag.StringVar(&options.sourceName, "source-name", "file", "source identifier for rows without source")
	flag.StringVar(&options.operation, "operation", "sale", "operation filter for listing sources")
	flag.StringVar(&options.property, "property", "apartment", "property filter for listing sources")
	flag.StringVar(&options.country, "country", "", "country code, for example UY")
	flag.IntVar(&options.maxListings, "max-listings", 0, "global maximum for multi-job live imports; defaults to -limit")
	flag.IntVar(&options.maxPages, "max-pages", 10, "maximum pages per search job")
	flag.BoolVar(&options.resume, "resume", false, "resume Gallito jobs from the progress file")
	flag.StringVar(&options.region, "region", "", "Gallito region or comma-separated regions")
	flag.StringVar(&options.progressFile, "progress-file", "var/gallito-progress.json", "Gallito progress file")
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
	case "file":
		listings, err := collectFile(options.input, options.format, options.sourceName)
		if err != nil {
			return collectedData{}, err
		}
		if len(listings) > options.limit {
			listings = listings[:options.limit]
		}
		return collectedData{listings: listings}, nil
	case "ciencuadras":
		listings, err := (ciencuadras.Source{Live: false}).Collect(ctx, options.limit)
		return collectedData{listings: listings}, err
	case "vixora":
		settings := config.Load()
		source := vixora.New(nil, settings.UserAgent, settings.Delay)
		source.Operation = options.operation
		source.PropertyType = options.property
		listings, err := source.Collect(ctx, options.limit)
		return collectedData{listings: listings}, err
	case "dccolombia":
		settings := config.Load()
		source := dccolombia.New(nil, settings.UserAgent, settings.Delay)
		listings, err := source.Collect(ctx, options.limit)
		return collectedData{listings: listings}, err
	case "gallito":
		if options.country != "" && strings.ToUpper(options.country) != "UY" {
			return collectedData{}, fmt.Errorf("gallito source supports only -country=UY")
		}
		settings := config.Load()
		source := gallito.New(nil, settings.UserAgent, settings.Delay)
		maxListings := options.maxListings
		if maxListings <= 0 {
			maxListings = options.limit
		}
		if options.maxPages <= 0 {
			return collectedData{}, fmt.Errorf("max-pages must be greater than zero")
		}
		jobs := gallitoJobs(options.region, options.operation, options.property)
		if len(jobs) == 0 {
			return collectedData{}, fmt.Errorf("no valid Gallito search jobs for region=%q", options.region)
		}
		progress := &gallito.Progress{}
		progressPath := ""
		if options.resume {
			progressPath = options.progressFile
			loaded, progressErr := gallito.LoadProgress(progressPath)
			if progressErr != nil {
				return collectedData{}, progressErr
			}
			progress = loaded
		}
		if options.resume || options.progressFile != "" {
			progressPath = options.progressFile
			if directory := filepath.Dir(progressPath); directory != "." {
				_ = os.MkdirAll(directory, 0o700)
			}
			source.ProgressHook = func(value *gallito.Progress) error { return gallito.SaveProgress(progressPath, value) }
		}
		listings, jobStats, err := source.CollectJobs(ctx, jobs, maxListings, options.maxPages, progress)
		for _, jobStat := range jobStats {
			slog.Info("Gallito import progress", "region", jobStat.Job.Region, "city", jobStat.Job.City,
				"operation", jobStat.Job.Operation, "property_type", jobStat.Job.PropertyType,
				"pages", jobStat.PagesFetched, "found", jobStat.ListingsFound,
				"accepted", jobStat.ListingsAccepted, "duplicates", jobStat.Duplicates, "last_page", jobStat.LastPage)
		}
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

func gallitoJobs(regionValue, operationsValue, propertiesValue string) []gallito.SearchJob {
	regions := map[string]gallito.SearchJob{
		"Montevideo":             {Region: "Montevideo", City: "Montevideo"},
		"Maldonado":              {Region: "Maldonado", City: "Maldonado", Department: "Maldonado"},
		"Punta del Este":         {Region: "Punta del Este", City: "Punta del Este", Department: "Maldonado"},
		"Canelones":              {Region: "Canelones", City: "Canelones", Department: "Canelones"},
		"Ciudad de la Costa":     {Region: "Ciudad de la Costa", City: "Ciudad de la Costa", Department: "Canelones"},
		"Colonia del Sacramento": {Region: "Colonia del Sacramento", City: "Colonia del Sacramento", Department: "Colonia"},
		"Piriápolis":             {Region: "Piriápolis", City: "Piriápolis", Department: "Maldonado"},
	}
	selectedRegions := make([]string, 0)
	if strings.TrimSpace(regionValue) == "" {
		for name := range regions {
			selectedRegions = append(selectedRegions, name)
		}
		sort.Strings(selectedRegions)
	} else {
		for _, name := range strings.Split(regionValue, ",") {
			name = strings.TrimSpace(name)
			if _, ok := regions[name]; ok {
				selectedRegions = append(selectedRegions, name)
			}
		}
	}
	operations := splitNonEmpty(operationsValue)
	properties := splitNonEmpty(propertiesValue)
	jobs := make([]gallito.SearchJob, 0, len(selectedRegions)*len(operations)*len(properties))
	for _, region := range selectedRegions {
		base := regions[region]
		for _, operation := range operations {
			for _, property := range properties {
				job := base
				job.Operation, job.PropertyType = operation, property
				jobs = append(jobs, job)
			}
		}
	}
	return jobs
}

func splitNonEmpty(value string) []string {
	result := make([]string, 0)
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
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

func collectFile(path, format, source string) ([]model.Listing, error) {
	if path == "" {
		return nil, fmt.Errorf("input is required for -source=file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open input file: %w", err)
	}
	defer file.Close()
	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
	switch format {
	case "json":
		return parser.ParseListingsJSON(file, source)
	case "csv":
		return parser.ParseListingsCSV(file, source)
	default:
		return nil, fmt.Errorf("unsupported input format %q; use json or csv", format)
	}
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
	stats, importErr := importer.Import(ctx, listings, store, false)
	if importErr != nil {
		_ = store.FinishRun(ctx, runID, "FAILED", stats.Read, stats.Created, stats.Updated, importErr)
		return importErr
	}
	if err := store.FinishRun(ctx, runID, "SUCCEEDED", stats.Read, stats.Created, stats.Updated, nil); err != nil {
		return err
	}
	slog.Info("collection complete", "source", source, "read", stats.Read,
		"accepted", stats.Accepted, "skipped", stats.Skipped,
		"created", stats.Created, "updated", stats.Updated,
		"snapshots_created", stats.SnapshotsCreated, "errors", stats.Errors)
	return nil
}
