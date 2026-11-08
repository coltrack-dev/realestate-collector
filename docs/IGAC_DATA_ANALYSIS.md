# IGAC transaction data analysis

## Scope

The IGAC dataset `7y2j-43cv` contains registered transactions, not property listings. The importer preserves source `pk` as `external_id` and stores `valor` in `property_transactions.transaction_value`. It does not assign `SALE`, `RENT` or another listing operation to a transaction.

The Bogotá import is constrained with `DIVIPOLA=11001`, stable `$order=pk`, and an optional `pk > ...` cursor for resuming.

## API field mapping

The importer maps all 26 catalog fields:

| API field | Database/model field | Meaning |
|---|---|---|
| `pk` | `external_id` | Stable IGAC record identifier |
| `matricula` | `matricula` | Registry folio |
| `numero_catastral_antiguo` | `old_cadastral_number` | Old cadastral number |
| `numero_catastral` | `current_cadastral_number` | Current cadastral number |
| `municipio`, `departamento`, `divipola`, `orip` | same | Geography and registry office |
| `fecha_radica_texto`, `fecha_apertura_texto`, `year_radica` | same | Filing/opening dates and year |
| `tipo_predio_zona`, `categoria_ruralidad_2024` | same | Zone and rurality classification |
| `num_anotacion`, `estado_folio`, `folios_derivados` | same | Folio/annotation data |
| `predios_nuevos`, `tiene_mas_de_un_valor` | same | New-folio and multiple-value flags |
| `valor`, `tiene_valor` | `transaction_value`, `has_value` | Registered transaction value and availability flag |
| `count_a`, `count_de` | same | Source counts |
| `documento_justificativo` | `justifying_document` | Supporting document |
| `cod_natujur`, `nombre_natujur` | `legal_act_code`, `legal_act_name` | Legal act code and label |
| `dinamica_2024` | `dynamic` | IGAC market-dynamics indicator |

Missing source values remain NULL/empty; they are not converted to zero.

## Semantics and quality rules

- `valor` is present only when the source indicates a value with `tiene_valor`; it is a registered value associated with the record, not an asking price.
- `cod_natujur` and `nombre_natujur` describe the registered legal act. A legal act can be a sale, mortgage, subdivision, adjudication or another registry event. The importer does not classify every row as a property sale.
- `dinamica_2024` is the IGAC indicator for acts considered relevant to real-estate dynamics; it is not an operation type.
- `predios_nuevos`, `tiene_mas_de_un_valor`, `count_a` and `count_de` are source indicators/counts and are preserved without interpretation.
- Rows without `pk` are skipped because the source identifier is required for idempotent import. No fallback based on address or a guessed composite key is used.

## Reproducible analysis SQL

Run these queries only against the isolated project database after applying migrations `001_init.sql` and `002_expand_igac_transactions.sql`:

```sql
SELECT year, count(*)
FROM property_transactions
WHERE source = 'igac-transactions' AND divipola = '11001'
GROUP BY year ORDER BY year;

SELECT legal_act_code, legal_act_name, count(*)
FROM property_transactions
WHERE source = 'igac-transactions' AND divipola = '11001'
GROUP BY legal_act_code, legal_act_name
ORDER BY count(*) DESC;

SELECT has_value, count(*)
FROM property_transactions
WHERE source = 'igac-transactions' AND divipola = '11001'
GROUP BY has_value;

SELECT
    count(*) AS total,
    count(*) FILTER (WHERE external_id IS NULL OR external_id = '') AS missing_pk,
    count(*) FILTER (WHERE divipola <> '11001') AS wrong_divipola,
    count(*) FILTER (WHERE legal_act_code IS NULL OR legal_act_code = '') AS missing_legal_code,
    count(*) FILTER (WHERE has_value = true AND transaction_value IS NULL) AS value_flag_without_value
FROM property_transactions
WHERE source = 'igac-transactions' AND divipola = '11001';
```

## Actual run status

No real records were imported in this environment.

### Environment verification — 2026-11-08

- Go: `go1.27.3 linux/amd64`.
- Docker client: `29.1.3`; Docker Compose v1 client: `1.29.2` (Docker CLI Compose plugin also reports `2.40.3`).
- `http_proxy=socks5h://192.168.13.128:4712`.
- `https_proxy=http://127.0.0.1:8118`.
- `NO_PROXY` is not set.
- Requests using the configured HTTPS proxy fail because the local proxy endpoint is unavailable.
- A direct HTTPS request with proxy variables unset failed at DNS resolution: `curl: (6) Could not resolve host: www.datos.gov.co`.
- Docker client cannot access the daemon: `permission denied while trying to connect to the Docker API at unix:///var/run/docker.sock`.
- `docker-compose ps` therefore cannot inspect or start the project PostgreSQL service.

No global proxy settings were changed. No PostgreSQL volume was removed or modified, and no database writes were attempted. Consequently, there are no factual year/legal-act/value distributions, SQL row counts, PostgreSQL errors, or idempotency results from a live run to report here; the queries above must be generated after network and isolated Docker access are available.

The attempted API URL was an anonymous request constrained by `divipola='11001'`, `$limit=1`, and `$order=pk`. It did not reach Socrata, so no real JSON types, `pk` range, or missing-value profile could be verified in this environment.

The code-level tests for the IGAC adapter pass, including all-field decoding, DIVIPOLA/cursor query construction, pagination, and cancellation. The full repository test command remains blocked by unavailable module archives for `goquery` and `pgx`.

The local fixture is only a parser test and is not included in real-data statistics.

## Targeted import controls

The IGAC adapter supports server-side filters for `year_radica`, `cod_natujur` or `nombre_natujur`, and `valor IS NOT NULL` via `-with-value`. It always requests `$order=pk`. `-batch-size` controls each page, while `-after-pk` supplies the last successfully persisted source identifier for continuation.

The import loop persists every record in a batch before advancing and logging `last_pk`. If a save fails or the process receives Ctrl+C, the cursor is not advanced for that incomplete batch; rerunning from the previous cursor is safe because `(source, external_id)` is unique and the upsert is idempotent.

Task 007 implementation was verified with local fake-HTTP tests. A live 1,000-record run was not started automatically and remains pending a normal terminal with working Socrata, PostgreSQL, and Go module access.

## Task 006 completeness queries

The official catalog identifies this as a 26-column Socrata table with annual updates and fields including `VALOR`, `TIENE_VALOR`, `YEAR_RADICA`, and `NOMBRE_NATUJUR`. The public catalog describes the dataset as registered transactions derived from registry records, not listing offers. The API must be queried with aggregates; downloading the complete dataset is unnecessary.

Use these commands from a normal terminal with a working network path. They do not write data:

```bash
API='https://www.datos.gov.co/resource/7y2j-43cv.json'
WHERE="divipola='11001'"

# Discover the actual JSON types and representative values.
curl --fail --silent --show-error --get "$API" \
  --data-urlencode '$select=year_radica,valor,tiene_valor,nombre_natujur' \
  --data-urlencode "\$where=$WHERE" \
  --data-urlencode '$limit=10' \
  --data-urlencode '$order=pk'

# All Bogotá records by year.
curl --fail --silent --show-error --get "$API" \
  --data-urlencode '$select=year_radica,count(*) as total' \
  --data-urlencode "\$where=$WHERE" \
  --data-urlencode '$group=year_radica' \
  --data-urlencode '$order=year_radica'

# COMPRAVENTA completeness by year. This assumes tiene_valor is a boolean.
curl --fail --silent --show-error --get "$API" \
  --data-urlencode '$select=year_radica,count(*) as total,count(valor) as valor_present,sum(case when valor is null then 1 else 0 end) as valor_null,sum(case when valor=0 then 1 else 0 end) as valor_zero,sum(case when valor>0 then 1 else 0 end) as valor_positive,sum(case when tiene_valor=true then 1 else 0 end) as tiene_valor_true' \
  --data-urlencode "\$where=${WHERE} AND upper(nombre_natujur)='COMPRAVENTA'" \
  --data-urlencode '$group=year_radica' \
  --data-urlencode '$order=year_radica'

# If the type discovery shows a text flag (for example SI/NO), use this variant.
curl --fail --silent --show-error --get "$API" \
  --data-urlencode '$select=year_radica,count(*) as total,count(valor) as valor_present,sum(case when valor is null then 1 else 0 end) as valor_null,sum(case when valor=0 then 1 else 0 end) as valor_zero,sum(case when valor>0 then 1 else 0 end) as valor_positive,sum(case when upper(tiene_valor) in ("SI","TRUE","1") then 1 else 0 end) as tiene_valor_true' \
  --data-urlencode "\$where=${WHERE} AND upper(nombre_natujur)='COMPRAVENTA'" \
  --data-urlencode '$group=year_radica' \
  --data-urlencode '$order=year_radica'
```

For a local live sample, use these read-only queries after importing exactly 1,000 records:

```sql
SELECT year, count(*)
FROM property_transactions
WHERE source = 'igac-transactions' AND divipola = '11001'
GROUP BY year ORDER BY year;

SELECT legal_act_code, legal_act_name, count(*)
FROM property_transactions
WHERE source = 'igac-transactions' AND divipola = '11001'
GROUP BY legal_act_code, legal_act_name
ORDER BY count(*) DESC;

SELECT year,
       count(*) AS total,
       count(*) FILTER (WHERE transaction_value IS NULL) AS value_null,
       count(*) FILTER (WHERE transaction_value = 0) AS value_zero,
       count(*) FILTER (WHERE transaction_value > 0) AS value_positive,
       count(*) FILTER (WHERE has_value IS TRUE) AS has_value_true
FROM property_transactions
WHERE source = 'igac-transactions'
  AND divipola = '11001'
  AND upper(legal_act_name) = 'COMPRAVENTA'
GROUP BY year ORDER BY year;
```

### Task 006 result status

The aggregate API requests and the local 1,000-record comparison were not executed successfully in this environment. The sandbox has no working route to the Socrata API, and the project PostgreSQL service is not reachable from the sandbox. Therefore no year-by-year counts, NULL/zero/positive value counts, `tiene_valor` counts, or local-vs-API comparison numbers are claimed here. The commands above are the reproducible next step in a normal Ubuntu terminal.

## Task 005 verification

The project `.env` was present and contains `DATABASE_URL`, `AUTH_MODE`, `USER_AGENT`, `REQUEST_DELAY`, `MAX_PAGES`, and `MAX_LISTINGS`. The application does not load `.env` itself; commands must export it, for example:

```bash
set -a
. ./.env
set +a
go run ./cmd/collector -source=igac-transactions -divipola=11001 -limit=10 -dry-run
```

The `.env` target was verified without exposing credentials as `realestate_db` on `127.0.0.1:5432` with user `realestate_user`. PostgreSQL was not listening on that address/port, so migrations and SQL checks were not executed.

The configured proxies were checked without changing system settings:

- `http_proxy=socks5h://192.168.13.128:4712` — connection refused/unavailable.
- `https_proxy=http://127.0.0.1:8118` — unavailable.
- A Socrata request through the existing SOCKS5 proxy failed with `curl: (7) Failed to connect to 192.168.13.128 port 4712`.

The Task 005 diagnostic request was constrained to `divipola='11001'`, `$limit=10`, and `$order=pk`, but no response was received. Therefore the real-record count, dry-run result, import result, field-type validation, duplicate check, repeated-import check, cursor check, and PostgreSQL statistics are all **not available**. No database writes were attempted.
