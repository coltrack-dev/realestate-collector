# Uruguay geographic coverage and source planning

## Status

Task 018V adds Uruguay geographic data to the same resolver used by Colombia.
No production PostgreSQL data was changed, no migration was applied, and no
live Gallito collection was run.

The geographic datasets describe where the map is, not where an enabled listing
source has permission or inventory. At the moment the automatic Uruguay source
registry is intentionally empty: Gallito remains `AllowsAutomatedRefresh=false`
until automated access and storage rights are confirmed. Consequently an
Uruguay viewport can resolve its geography, but it produces zero permitted
external source queries and keeps existing PostgreSQL listings visible.

## Local datasets

| File | Official source | Content | CRS used by project | Count |
|---|---|---|---|---:|
| `data/geo/uruguay_departments.geojson` | IDE Uruguay metadata record [08cb22f3-ddb6-4c33-a300-d9a36998b6f0](https://visualizador.ide.uy/geonetwork/srv/api/records/08cb22f3-ddb6-4c33-a300-d9a36998b6f0), WFS `ws_uamp/departamentos` | Departments | EPSG:4326 output; source metadata documents EPSG:3857 | 19 |
| `data/geo/uruguay_municipalities.geojson` | IDE Uruguay metadata record [82c71a84-97ac-4cf1-8ac9-c0296b2383ad](https://visualizador.ide.uy/geonetwork/static/api/records/82c71a84-97ac-4cf1-8ac9-c0296b2383ad), WFS `ideuy/municipios_20250507` | Municipalities 2025 | EPSG:4326 | 136 |
| `data/geo/uruguay_localities.geojson` | IDE layer [localidades_censales_ine](https://visualizador.ide.uy/geonetwork/srv/resources/datasets/ideuy%3Alocalidades_censales_ine), WFS `ideuy/localidades_censales_ine` | INE Census 2023 localities | EPSG:4326 | 652 |

The IDE geoservices are published as Uruguay open data under the [IDE open
geoservices information](https://www.gub.uy/infraestructura-datos-espaciales/politicas-y-gestion/geoservicios-ide).
The department metadata is dated 2020; the municipalities layer is the 2025
version published in December 2025. The locality layer is the INE Census 2023
cartography exposed through IDE and is described as based on 2011 census zones
adjusted with imagery and other official sources. INE cartographic products can
be revised or temporarily unavailable; the project therefore records the IDE
layer provenance rather than claiming that it is an immutable legal boundary.
The INE locality code convention is documented in the [Census 2023
definitions](https://www5.ine.gub.uy/documents/Cartografía/Censo%202023/DOCUMENTOS/Definiciones.pdf).

The WFS responses were converted once by
`scripts/geo/convert_uruguay_ide.py`. The converter preserves source names and
identifiers, rejects invalid geometry types, and excludes the IDE service
feature `LIMITE CONTESTADO` from the department administrative count because it
has no department identifier. No artificial rectangles were added.

## Resolver and planner

`geo.NewFromFiles` loads the Colombia municipality file and the three Uruguay
files together. The resolver returns the deepest point match and fills the
other available levels from containing polygons:

`country → department → municipality → locality`

Viewport responses include `areas` with `level`, original identifiers,
overlap area and overlap ratio. `municipalities` remains in the response for
backwards compatibility. Polygon/MultiPolygon bounding-box prefiltering and
rectangle clipping are used before calculating overlap in a latitude-adjusted
metric projection. Polygon holes are preserved by the existing geometry parser.

The planner selects the most precise filter supported by an enabled source:
locality, municipality, department, or bbox. It never treats a locality as a
municipality and never falls back to a nationwide request. It retains bounded
fan-out, source-query cache, deduplication and rate limits. Gallito is not an
enabled automatic source.

Startup configuration is independent by country:

```text
GEO_BOUNDARIES_PATH=data/geo/colombia_municipalities.geojson
GEO_URUGUAY_DEPARTMENTS_PATH=data/geo/uruguay_departments.geojson
GEO_URUGUAY_MUNICIPALITIES_PATH=data/geo/uruguay_municipalities.geojson
GEO_URUGUAY_LOCALITIES_PATH=data/geo/uruguay_localities.geojson
```

The API logs total counts and counts by `country:level` at startup.

## Verified geographic cases

Tests resolve Montevideo, Maldonado, Punta del Este, Piriápolis, Colonia del
Sacramento and Ciudad de la Costa to Uruguay departments/localities while
retaining Colombia resolution for Bogotá. Tests also cover a Uruguay viewport
with overlap metrics and verify that Uruguay does not use DIVIPOLA.

These are geographic checks only. They do not prove that Gallito or another
provider has inventory in each place.

## Source coverage matrix

| Source | Country | Confirmed geographic coverage in this implementation | Supported filters | Automatic refresh | Status |
|---|---|---|---|---|---|
| Gallito MCP/REST | UY | Not established by the resolver | Requires separate confirmation of department/municipality/locality/bbox contract | **Disabled** | No permitted source query is planned |
| VIXORA | CO | Existing Colombia registry and adapter | Existing municipality/operation/type contract | Existing policy | Unchanged by this task |
| D.C Colombia | CO | Existing Colombia registry and adapter | Existing city/operation/type contract | Existing policy | Unchanged by this task |

“Geographic coverage” above means the local boundary can identify the place;
“source coverage” means an enabled provider has confirmed inventory, query
semantics and permission for automated retrieval. The latter remains
unconfirmed for Uruguay in this task.

## Checks

The relevant resolver test is:

```bash
GOCACHE=/tmp/realestate-go-build-cache /usr/local/go/bin/go test ./internal/geo -run TestUruguayHierarchyAndColombiaRemainAvailable -v
```

The repository-wide Go and browser checks must be run from an environment that
allows the Go build cache and loopback mock servers. In the restricted Codex
sandbox, the system `go` command is a snap wrapper that fails before running,
and `httptest.Server` loopback listeners are denied; this is an environment
limitation, not a live-source result.
