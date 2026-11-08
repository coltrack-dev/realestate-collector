# Real Estate Collector

Go MVP for reproducible collection and change tracking of Colombian real-estate listings.

The current working collector is `fixture`; it parses `testdata/fixtures/listings.json`, normalizes values and can persist them to PostgreSQL. Ciencuadras live collection is intentionally disabled because its terms expressly prohibit automated extraction. FincaRaíz remains pending permission/API confirmation; see [`docs/SOURCE_RESEARCH.md`](docs/SOURCE_RESEARCH.md).

Authentication defaults to `AUTH_MODE=none`. Observed search pages were publicly viewable without registration, so the MVP contains no login flow. `AUTH_MODE=session` is reserved for a future explicitly authorized source and does not support Google OAuth, browser cookies, browser profiles or CAPTCHA automation.

## First permitted live source

IGAC's registered-property-transactions dataset on `datos.gov.co` is integrated as `igac-transactions`. It is public Socrata JSON data, not portal listings. Values are stored separately in `property_transactions` and must not be treated as asking prices.

```bash
go run ./cmd/collector -source=igac-transactions -divipola=11001 -limit=100 -dry-run
```

To resume after a known IGAC record, add `-after-pk=<last-pk>`. The adapter uses `$order=pk` and retains the source `pk` as the idempotency key.

Targeted historical import (manual start, maximum 1000 records):

```bash
go run ./cmd/collector \
  -source=igac-transactions \
  -divipola=11001 \
  -year-from=2021 \
  -year-to=2023 \
  -legal-act-name=COMPRAVENTA \
  -with-value \
  -batch-size=100 \
  -limit=1000
```

The collector logs `read`, `saved`, `updated`, `skipped`, `last_pk`, and processing speed after each successfully persisted batch. If interrupted, rerun with the last logged `-after-pk`; the cursor advances only after the complete batch is saved.

For database import, apply `migrations/001_init.sql` and then `migrations/002_expand_igac_transactions.sql` to the isolated Compose database, set `DATABASE_URL`, omit `-dry-run`, and use a small limit first. See [`docs/SOURCE_CANDIDATES.md`](docs/SOURCE_CANDIDATES.md) and [`docs/IGAC_DATA_ANALYSIS.md`](docs/IGAC_DATA_ANALYSIS.md).

## Run

```bash
cp .env.example .env
docker compose up -d
/usr/local/go/bin/go mod tidy
/usr/local/go/bin/go run ./cmd/collector -source=fixture -limit=20 -dry-run
```

Apply `migrations/001_init.sql` using an isolated Compose database before persistence. `-dry-run` never connects to PostgreSQL.

The design uses `Source`, parser/normalizer, PostgreSQL upsert and content-hash snapshots. The schema guarantees `(source_id, external_id)` uniqueness and suppresses duplicate snapshots.

## Universal local listings importer

The `file` source imports local JSON or CSV without contacting an external site.
Rows use the `Listing` fields (snake_case or Go-style names), and rows without a
`source` use `-source-name`. Price values are stored as integer source units;
currency is preserved and no conversion is performed.

JSON dry-run:

```bash
go run ./cmd/collector \
  -source=file \
  -input=testdata/fixtures/listings.json \
  -format=json \
  -source-name=fixture-json \
  -limit=20 \
  -dry-run
```

CSV import into the configured isolated PostgreSQL database:

```bash
go run ./cmd/collector \
  -source=file \
  -input=testdata/fixtures/listings.csv \
  -format=csv \
  -source-name=agency-feed \
  -limit=100
```

The importer reports read, accepted, skipped, created, updated, snapshot and
validation-error counts. A repeated import is idempotent; the PostgreSQL store
creates a new `listing_snapshots` row only when the content hash changes.
External listing sources remain disabled until automated access and data reuse
are explicitly permitted. See [`docs/LISTINGS_SOURCES_RESEARCH.md`](docs/LISTINGS_SOURCES_RESEARCH.md)
and [`docs/MERCADO_LIBRE_COLOMBIA.md`](docs/MERCADO_LIBRE_COLOMBIA.md).

## Live listings MVP

VIXORA and D.C Colombia expose documented public read APIs. The adapters use
only those APIs, do not submit credentials or contact data, and keep requests
sequential with a bounded rate limit.

```bash
go run ./cmd/collector -source=vixora -limit=30 -dry-run
go run ./cmd/collector -source=dccolombia -limit=50 -dry-run
```

After checking the isolated `DATABASE_URL`, omit `-dry-run` for a limited
import. The first verified import stored 30 VIXORA and 50 D.C Colombia listings;
the repeated import created no duplicates and no additional snapshots. See
[`docs/LIVE_LISTINGS_MVP.md`](docs/LIVE_LISTINGS_MVP.md).

## Read-only listings API

Start the local API with the isolated database configured in `DATABASE_URL`:

```bash
go run ./cmd/api
```

It listens on `127.0.0.1:8086` by default. Available endpoints:

```text
GET /health
GET /api/v1/listings?source=vixora&city=Bogotá%20D.C.&operation=SALE&property_type=APARTMENT&limit=50&offset=0
GET /api/v1/listings/{id}
GET /api/v1/listings/{id}/history
GET /api/v1/stats
```

The API is read-only, uses parameterized PostgreSQL queries, applies filters in
PostgreSQL, and caps pages at 100 records. It does not expose contact details.

## Web UI

```bash
cd web
npm install
npm run dev
```

Vite listens on `127.0.0.1:5173` and proxies `/api` and `/health` to the API at
`127.0.0.1:8086`.
# realestate-collector
