# Source Coverage Audit

Дата проверки: 2026-11-09. Массовый live-import и запись в рабочую PostgreSQL не
выполнялись. Live-проверка D.C Colombia ограничена четырьмя GET-запросами с
узкими фильтрами; ответы агрегированы локально.

## Итог

Cartagena не была пустой у D.C Colombia. Причина отсутствия новых объявлений в
приложении была в регистре параметров адаптера: coordinator передавал
`SALE/APARTMENT`, а API D.C Colombia ожидает `sale/apartment`. Запрос с
uppercase возвращал HTTP 200 и `{"properties":[]}`; эквивалентный запрос с
lowercase вернул 4 объявления `Cartagena de Indias`.

До исправления последовательность была такой:

```text
MapLibre moveend
  -> geo/resolve: CO / Cartagena / dccolombia
  -> POST source-refresh: SALE / APARTMENT
  -> coordinator: выбран dccolombia, VIXORA исключён (муниципалитет не поддержан)
  -> D.C adapter: operation=SALE&property_type=APARTMENT
  -> HTTP 200 {"properties":[]}
  -> adapter: успешная пустая выдача
  -> Store.Save: 0 вызовов
  -> /stats: без изменений
```

После исправления adapter преобразует внутренние значения в контракт источника:
`SALE -> sale`, `RENT -> rent`, `APARTMENT -> apartment`, `HOUSE -> house`,
`LAND -> lot`, `COMMERCIAL -> commercial`. Пустые значения остаются пустыми и
означают ALL.

## Матрица покрытия

| Source | Country | Подтверждённые муниципалитеты | Фильтры | Ограничения API | Разрешение auto-refresh | Статус данных | Evidence |
|---|---|---|---|---|---|---|---|
| D.C Colombia | CO | Cartagena de Indias, Bogotá D.C., Cali и другие города из текущего inventory; Cartagena подтверждена live | `city`, `operation=sale/rent`, `property_type=apartment/house/office/commercial/lot/warehouse/farm`; пустые фильтры допустимы технически | До 200 результатов, публичный ответ без pagination-контракта; для high-volume документация предлагает связаться с компанией | Публичный GET подтверждён; разрешение на регулярное высокообъёмное хранение не подтверждено, поэтому refresh остаётся ограниченным | **Live подтверждено:** 4 apartment-sale для Cartagena; 14 при одном фильтре city | [официальная API-документация](https://www.dccolombia.com.co/docs), [условия](https://www.dccolombia.com.co/terminos) |
| VIXORA | CO | Oriente Antioqueño: El Peñol, El Retiro, Guarne, Guatapé, La Ceja, Marinilla, Rionegro, San Vicente Ferrer в текущем registry | `municipality`, `type`, `operation`, pagination; public API | `page_size` максимум 50; inventory не заявлен как национальный; Cartagena не подтверждена | Public read-only API без ключа, но хранение цен должно быть краткосрочным и с ссылкой на источник | **Cartagena не покрыта**; источник исключается до HTTP | [developer API](https://vixoraai.com/desarrolladores), [terms](https://vixoraai.com/legal/terminos) |
| Gallito MCP | UY | Uruguay jobs из существующего адаптера | MCP search/more_results; country UY | Автоматический refresh отключён политикой проекта до подтверждения условий массовой агрегации | **Запрещён в automatic refresh текущей конфигурацией** | Не источник Cartagena | [исследование Uruguay](URUGUAY_SOURCES.md) |

«Подтверждённый муниципалитет» означает фактическое поле `city` в live или
fixture-ответе, а не только наличие записи в Source Registry. Registry сообщает
техническую совместимость источника, но не доказывает наличие объектов.

## Фактические live-проверки D.C Colombia

Выполнены только следующие GET-запросы:

```text
GET /api/properties?city=Cartagena&operation=sale&property_type=apartment
GET /api/properties?city=Cartagena%20de%20Indias&operation=sale&property_type=apartment
GET /api/properties?city=Cartagena
GET /api/properties?city=Cartagena&operation=SALE&property_type=APARTMENT
```

Результаты:

| Query | HTTP | Properties | Cities | Operations/types |
|---|---:|---:|---|---|
| Cartagena + lowercase sale/apartment | 200 | 4 | Cartagena de Indias | sale/apartment |
| Cartagena de Indias + lowercase sale/apartment | 200 | 4 | Cartagena de Indias | sale/apartment |
| Cartagena only | 200 | 14 | Cartagena de Indias | sale/rent × apartment/house/commercial/lot |
| Current pre-fix uppercase query | 200 | 0 | — | — |

Следовательно, `{"properties":[]}` в данном случае является корректным HTTP
200 пустым результатом для неверных значений фильтров, а не доказательством
отсутствия Cartagena в inventory.

## Что отбрасывалось

В проблемном live-запросе adapter не отбрасывал записи нормализацией: API уже
вернул пустой массив, поэтому `FetchedCount=0`, `RejectedCount=0`, `Save` не
вызывался. После исправления mapping fixture-тест передаёт запись через
`toListing`; записи без стабильного `slug/id` по-прежнему считаются rejected.

## Изолированное доказательство pipeline

Добавлен тест `TestRefreshPipelineReachesListingsAndStats` в
`internal/api/server_test.go`. Он использует mock source и in-memory repository:

1. mock source возвращает `CART-1`;
2. coordinator вызывает `Save` и получает `inserted=1`;
3. `GET /api/v1/listings` возвращает `CART-1`;
4. `GET /api/v1/stats` возвращает `total=1`.

Frontend browser mock также подтверждает, что после завершения refresh Vue
перечитывает локальные listings/stats. Рабочая PostgreSQL в этом тесте не
используется.

## Исправленные файлы

- `internal/sources/dccolombia/source.go` — mapping внутренних enum в значения API;
- `internal/sources/dccolombia/source_test.go` — тесты Cartagena, ALL и query mapping;
- `internal/api/server_test.go` — изолированный pipeline source → Save → listings/stats.

Проверки: `go test ./...`, `go vet ./...`, `go build ./cmd/api ./cmd/collector`,
`npm run build`, `npm audit` и `git diff --check` выполняются в изолированном
окружении; live-import в `realestate_db` не выполнялся.

## Ограничения и следующий шаг

Live-ответ подтверждает наличие 4 объектов на момент проверки, но не даёт
основания считать D.C Colombia полным рынком Cartagena. API возвращает максимум
200 записей без подтверждённого cursor/page механизма, а политика хранения и
регулярного автоматизированного доступа для high-volume требует отдельного
согласования с владельцем источника. Следующий разрешённый шаг — один
контролируемый импорт этих 4 записей в изолированную проектную БД после отдельного
подтверждения, с проверкой повторного upsert и `/stats`.
