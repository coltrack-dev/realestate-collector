# Viewport-to-municipalities planner

## Что реализовано

`internal/geo` теперь поддерживает `ViewportResolver`. Для `POST /api/v1/geo/resolve-viewport` принимается объект:

```json
{
  "bbox": {
    "min_longitude": -75.2,
    "max_longitude": -74.8,
    "min_latitude": 6.1,
    "max_latitude": 6.4
  }
}
```

Результат содержит все муниципальные Polygon/MultiPolygon, пересекающие bbox, с `country_code`, департаментом, муниципалитетом, `divipola`, площадью пересечения в квадратных метрах и отношением площади пересечения к площади муниципалитета.

Пересечение вычисляется отсечением полигонов по четырём сторонам bbox. Внутренние кольца вычитаются, поэтому отверстия не считаются территорией муниципалитета. Площадь вычисляется в локальной равноугольной метрической проекции с поправкой на широту, а не в градусах.

## Планирование источников

`POST /api/v1/source-refresh/viewport` сначала разрешает bbox в муниципалитеты, затем строит независимый запрос для каждого поддерживаемого муниципалитета. `refresh.Coordinator.RequestViewport`:

- ставит муниципалитет в центре viewport первым;
- затем сортирует остальные по `overlap_ratio` и площади пересечения;
- дедуплицирует по фактическому source-query identity;
- применяет source-query cache, inflight deduplication и source rate limits;
- ограничивает новые source queries параметром `max_queries` (максимум 3);
- возвращает явные причины пропуска, включая `max_source_queries_per_viewport` и `no_supported_source_or_recently_checked`.

City-only источники не получают ложного полного bbox coverage: результат остаётся partial, а cache сохраняет только факт успешной проверки конкретного набора внешних фильтров.

Gallito не включается автоматически — это определяется его существующей capability policy.

## Проверяемый fixture-сценарий

Для bbox выше тестовые полигоны `Rionegro` и `Marinilla` возвращаются одновременно с положительными `overlap_area_m2`, `overlap_ratio` и DIVIPOLA. При лимите `max_queries=1` первым выбирается муниципалитет, совпадающий с центром viewport; остальные получают причину ограничения fan-out. Повторный запрос того же муниципалитета после успешной partial-проверки получает cache skip без нового source job.

Live-import и продуктивная PostgreSQL для этой проверки не использовались.
