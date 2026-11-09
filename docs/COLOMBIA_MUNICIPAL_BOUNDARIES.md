# Colombia municipal boundaries

## Local snapshot

The production default is:

```text
data/geo/colombia_municipalities.geojson
```

The file is a one-time local snapshot of the official DANE `DIVIPOLA MGN 2024`
ArcGIS FeatureServer, layer `Municipio` (317). The service exposes GeoJSON/
ArcGIS JSON, the DIVIPOLA fields and municipal polygon geometry. The snapshot
was requested with `outSR=4326` and server-side `maxAllowableOffset=0.001`
degrees to keep the local resolver bounded in size; this is suitable for
municipality resolution and viewport planning, not cadastral surveying.

Official evidence:

- [DANE MGN 2024 FeatureServer](https://geoportal.dane.gov.co/mparcgis/rest/services/Divipola/Serv_DIVIPOLA_MGN_2024/FeatureServer)
- [DANE Municipio layer](https://geoportal.dane.gov.co/mparcgis/rest/services/Divipola/Serv_DIVIPOLA_MGN_2024/FeatureServer/317)
- [IGAC open-data policy](https://www.igac.gov.co/datos-abiertos/)
- [Colombia en Mapas licence guidance](https://colombia-mapas.gitbook.io/colombia-en-mapas/preguntas-frecuentes-faq)

DANE's service is public and the IGAC/Colombia en Mapas guidance describes
open geographic data as reusable with attribution when the dataset is marked
CC BY 4.0. The DANE layer metadata itself did not expose a populated licence
field during this snapshot. Therefore the project preserves attribution and
source/version metadata and does not claim a broader licence than the official
terms support.

## Validation results

- Features: **1,121**.
- Unique DIVIPOLA codes: **1,121**.
- Entity types: 1,102 `MUNICIPIO`, 18 `ÁREA NO MUNICIPALIZADA`, 1 `ISLA`.
- Geometry types: 1,112 Polygon and 9 MultiPolygon.
- Snapshot version: DANE `MPIO_NANO=2024`.
- Baranoa: `CO / ATLÁNTICO / BARANOA / 08078`.
- Cartagena: `CO / BOLÍVAR / CARTAGENA DE INDIAS / 13001`.
- Bogotá: `CO / BOGOTÁ, D.C. / BOGOTÁ, D.C. / 11001`.
- Barranquilla: `CO / ATLÁNTICO / BARRANQUILLA / 08001`.
- Medellín: `CO / ANTIOQUIA / MEDELLÍN / 05001`.
- Rionegro: `CO / ANTIOQUIA / RIONEGRO / 05615`.

The resolver validates geometry type, non-empty rings, unique five-digit
DIVIPOLA codes and expected coordinate ranges in tests. It also stores a
bounding box per feature and rejects that box before exact polygon clipping,
so viewport resolution does not run a full polygon intersection for every
municipality.

The converter used to create the snapshot is
`scripts/geo/convert_dane_arcgis.py`. It is intentionally not part of API
startup: updating the dataset is a controlled, reviewed operation.

## Run

From the project root:

```bash
GEO_BOUNDARIES_PATH=data/geo/colombia_municipalities.geojson \
  go run ./cmd/api
```

The API logs a line similar to:

```text
geo_boundaries_loaded count=1121 source=data/geo/colombia_municipalities.geojson version=colombia_municipalities_dane_mgn_2024
```
