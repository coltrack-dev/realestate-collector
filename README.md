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

Listing images remain external URLs by default (`IMAGE_STORAGE_MODE=EXTERNAL_URL`).
VIXORA detail responses are fetched lazily for an opened listing to preserve the complete ordered image list;
D.C Colombia currently exposes only the confirmed `cover` field in its public
JSON response. Migration `003_listing_images.sql` prepares per-image positions,
hashes, MIME metadata, temporary-cache expiry and revocation fields without
storing image bytes in PostgreSQL. `TEMP_CACHE` and `LOCAL_STORAGE` are
available as explicit future modes; no live image download is started by the
collector automatically.

Before a database-backed listings import, apply `migrations/003_listing_images.sql`
after the existing migrations. The collector does not run migrations
automatically.

Property details are lazy: VIXORA search imports only Summary data and does not
call a detail endpoint. The read API serves saved data from PostgreSQL; the UI
may request `POST /api/v1/listings/{id}/refresh-details` for one opened listing.
Details are cached for `DETAILS_TTL` (default `24h`), concurrent refreshes are
deduplicated, and failed retries have a cooldown. Apply
`migrations/004_lazy_listing_details.sql` manually before starting the API.

Location fallback is separate from source coordinates. Migration
`007_listing_geocoding.sql` adds `resolved_latitude`, `resolved_longitude`,
`location_precision`, provider and timestamp fields; it is not applied
automatically. The original `latitude`/`longitude` values are never overwritten.
The current local geocoder resolves only curated city, department and country
centers. It does not perform mass external geocoding and does not represent an
administrative center as an exact property marker.

## Uruguay

The first Uruguay source is the public Inmuebles El País / Gallito MCP adapter.
It uses the official Streamable HTTP endpoint without OAuth for search and
public listing details:

```bash
go run ./cmd/collector \
  -source=gallito -city=Montevideo -operation=sale \
  -property=apartment -limit=20 -dry-run
```

Для подготовленного multi-job режима доступны `-country=UY`,
`-max-listings`, `-max-pages`, `-region`, `-resume` и
`-progress-file`. Он не запускается автоматически и предназначен для работы
после подтверждения лимитов и условий хранения источника.

Apply `migrations/005_listing_country_code.sql` and
`migrations/006_backfill_country_code.sql` manually before importing Uruguay.
The adapter stores `country_code=UY`, preserves USD/UYU without conversion,
uses the MCP listing id as `external_id`, and does not collect agency contact
details. See [`docs/URUGUAY_SOURCES.md`](docs/URUGUAY_SOURCES.md) for access and
retention limitations.

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
POST /api/v1/listings/{id}/refresh-details
GET /api/v1/stats
POST /api/v1/source-refresh
GET /api/v1/source-refresh/{id}

GET /api/v1/geo/resolve?longitude=-74.07&latitude=4.71
```

Listing reads are read-only and use parameterized PostgreSQL queries, with pages
capped at 100 records. `POST /api/v1/source-refresh` only queues a bounded,
asynchronous refresh for configured sources; it never accepts arbitrary external
URLs. The required city is resolved from the current map viewport. Empty
`operation` and/or `property_type` values explicitly mean `ALL`; the server
passes that only to sources declaring support for unfiltered searches and keeps
the plan bounded to the registered source queries. VIXORA and D.C Colombia are
throttled per source;
Gallito is registered but disabled until automated aggregation is explicitly
permitted. Source refresh creation is disabled by default; set
`SOURCE_REFRESH_ENABLED=true` only in a trusted, protected deployment. The
endpoint is intended for localhost or a protected deployment and does not expose
contact details.

Source refreshes use a bounded source-query cache (default TTL `15m`, maximum
`128` keys). For city-only providers such as D.C Colombia, small map movements
reuse the same normalized source query and do not imply that the original bbox
is fully covered. Configure `SOURCE_QUERY_CACHE_TTL` and
`SOURCE_QUERY_CACHE_MAX` when needed. A cache hit returns `SKIPPED` with reason
`source_query_cache_hit`, while the frontend keeps showing PostgreSQL results.

Geographic resolution uses the checked-in DANE MGN 2024 municipality snapshot at
`data/geo/colombia_municipalities.geojson` by default. Override it with
`GEO_BOUNDARIES_PATH` when using another explicitly validated GeoJSON source.
Each feature provides the original DIVIPOLA code, department and municipality
names, and Polygon or MultiPolygon geometry in EPSG:4326. On startup the API
logs `geo_boundaries_loaded` with the feature count, source path and dataset
version. The snapshot contains 1,121 DANE entities, including 1,102
municipalities and 18 áreas no municipalizadas; it is locally stored and is not
downloaded at API startup. If the file is not configured or unavailable,
`/api/v1/geo/resolve` returns `resolution=UNKNOWN` and the map does not invent a
municipality or start a nationwide refresh.

External request and refresh lifecycle logging is documented in
[`docs/HTTP_LOGGING.md`](docs/HTTP_LOGGING.md). HTTP request starts are DEBUG;
completed requests and refresh summaries use structured `slog` fields.

## Web UI

```bash
cd web
npm install
npm run dev
```

Vite listens on `127.0.0.1:5173` and proxies `/api` and `/health` to the API at
`127.0.0.1:8086`.
# realestate-collector
