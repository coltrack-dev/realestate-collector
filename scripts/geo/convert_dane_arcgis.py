#!/usr/bin/env python3
"""Convert one DANE ArcGIS JSON snapshot to the project's GeoJSON schema."""

import json
import sys
from pathlib import Path


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit("usage: convert_dane_arcgis.py INPUT OUTPUT")
    source = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
    features = []
    seen = set()
    for item in source.get("features", []):
        attrs = item.get("attributes", {})
        code = str(attrs.get("MPIO_CDPMP", "")).strip()
        rings = item.get("geometry", {}).get("rings", [])
        if len(code) != 5 or not rings or code in seen:
            raise ValueError(f"invalid or duplicate feature: {code!r}")
        seen.add(code)
        # ArcGIS uses clockwise outer rings and counter-clockwise holes. A
        # multipart municipality may therefore contain several outer rings.
        converted = [[[float(point[0]), float(point[1])] for point in ring] for ring in rings]
        if any(len(ring) < 4 for ring in converted):
            raise ValueError(f"invalid ring in {code}")
        polygons = []
        for ring in converted:
            area = sum(ring[i][0] * ring[(i + 1) % len(ring)][1] - ring[(i + 1) % len(ring)][0] * ring[i][1] for i in range(len(ring))) / 2
            if area < 0:
                polygons.append([ring])
            elif polygons:
                polygons[-1].append(ring)
            else:
                raise ValueError(f"hole precedes outer ring in {code}")
        geometry = {"type": "Polygon", "coordinates": polygons[0]}
        if len(polygons) > 1:
            geometry = {"type": "MultiPolygon", "coordinates": polygons}
        features.append({
            "type": "Feature",
            "properties": {
                "country_code": "CO",
                "department": attrs.get("DPTO_CNMBRE", ""),
                "municipality": attrs.get("MPIO_CNMBRE", ""),
                "divipola": code,
                "entity_type": attrs.get("MPIO_TIPO", ""),
                "dane_version": attrs.get("MPIO_NANO"),
            },
            "geometry": geometry,
        })
    if len(features) != 1121:
        raise ValueError(f"expected DANE MGN 2024 feature count 1121, got {len(features)}")
    document = {
        "type": "FeatureCollection",
        "name": "colombia_municipalities_dane_mgn_2024",
        "crs": {"type": "name", "properties": {"name": "urn:ogc:def:crs:OGC:1.3:CRS84"}},
        "features": features,
    }
    Path(sys.argv[2]).parent.mkdir(parents=True, exist_ok=True)
    Path(sys.argv[2]).write_text(json.dumps(document, ensure_ascii=False, separators=(",", ":")) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
