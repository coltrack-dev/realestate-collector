# External HTTP logging

Внешние запросы VIXORA, D.C Colombia и Gallito проходят через общий
`http.RoundTripper` в `internal/sources/httpclient`.

Транспорт пишет структурированные события `slog`:

- `external_http_start` — DEBUG, до каждой фактической попытки;
- `external_http_complete` — INFO для успешных ответов и WARN для HTTP 4xx/5xx,
  timeout и сетевых ошибок.

В контексте refresh передаются `source`, `refresh_id`, `operation`, `city` и
`country_code`. Каждая повторная попытка получает отдельный `attempt`. Для HTTP
429 дополнительно указываются `retry_after`, рассчитанная задержка и
`next_allowed_at`.

Логируется только host и path. Query string, заголовки, cookies, токены, ключи
и тела запросов/ответов не записываются. Размер ответа берётся из
`Content-Length`, если он известен.

Логические события координатора (`refresh_requested`, `refresh_queued`,
`refresh_started`, `refresh_completed`, `refresh_failed`, `refresh_cooldown`,
`refresh_cache_hit`, `refresh_deduplicated`) отделены от событий отдельных
HTTP-попыток. Gallito остаётся отключённым для автоматического refresh по
политике источника.

Для повторных запросов без bbox используется bounded source-query cache. Его
события: `source_query_cache_hit`, `source_query_cache_miss`,
`source_query_cache_store` и `source_query_inflight_deduplicated`. По умолчанию
TTL составляет 15 минут, размер — 128 записей; настройки задаются через
`SOURCE_QUERY_CACHE_TTL` и `SOURCE_QUERY_CACHE_MAX`. Для D.C Colombia ключ
включает источник, муниципалитет, operation и property type, но не bbox.
