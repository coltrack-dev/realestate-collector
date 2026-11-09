#!/usr/bin/env python3
"""Normalize an official IDE Uruguay GeoJSON layer for the project resolver."""

import json
import sys
from pathlib import Path


def main() -> None:
    if len(sys.argv) != 4:
        raise SystemExit("usage: convert_uruguay_ide.py LEVEL INPUT OUTPUT")
    level, input_path, output_path = sys.argv[1], Path(sys.argv[2]), Path(sys.argv[3])
    source = json.loads(input_path.read_text(encoding="utf-8"))
    features = []
    for item in source.get("features", []):
        raw = item.get("properties", {})
        geometry = item.get("geometry")
        if not geometry or geometry.get("type") not in {"Polygon", "MultiPolygon"}:
            raise ValueError(f"invalid geometry in {input_path}")
        if level == "DEPARTMENT":
            code = str(raw.get("codigo_iso") or "").strip()
            if not code:  # IDE layer also contains the service boundary label.
                continue
            properties = {"country_code": "UY", "level": level, "department": raw.get("nombre", ""), "department_id": code}
        elif level == "MUNICIPALITY":
            code = str(raw.get("cod_muni") or "").strip()
            properties = {"country_code": "UY", "level": level, "department": raw.get("depto", ""), "municipality": raw.get("municipio", ""), "municipality_id": code}
        elif level == "LOCALITY":
            code = str(raw.get("cloc_iso") or raw.get("codloc") or "").strip()
            properties = {"country_code": "UY", "level": level, "department": raw.get("nomdepto", ""), "locality": raw.get("nomloc", ""), "locality_id": code}
        else:
            raise ValueError(f"unsupported level: {level}")
        if not code or not properties.get(level.lower() if level != "DEPARTMENT" else "department"):
            raise ValueError(f"missing identity in {input_path}: {raw}")
        features.append({"type": "Feature", "properties": properties, "geometry": geometry})
    if len({json.dumps(feature["properties"], sort_keys=True) for feature in features}) != len(features):
        raise ValueError(f"duplicate properties in {input_path}")
    document = {"type": "FeatureCollection", "name": f"uruguay_{level.lower()}_ide", "features": features}
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(document, ensure_ascii=False, separators=(",", ":")) + "\n", encoding="utf-8")
    print(f"{level}: {len(features)} features -> {output_path}")


if __name__ == "__main__":
    main()
