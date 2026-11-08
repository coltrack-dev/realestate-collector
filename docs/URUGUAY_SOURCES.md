# Uruguay sources research

Проверка выполнена 2026-11-09. Основной выбранный источник — публичный MCP
сервер Inmuebles El País / Gallito:

`https://inmuebles.elpais.com.uy/api/public-mcp`

## Сравнение кандидатов

| Источник | Доступ | Авторизация | Данные | Пагинация и ограничения | Решение |
|---|---|---|---|---|---|
| Inmuebles El País / Gallito public MCP | Streamable HTTP JSON-RPC MCP | `noauth` для поиска и публичной карточки; OAuth 2.1 только для favorites, saved searches и enquiries | listings: id, title, operation, type, price/currency, area, rooms, bathrooms, city, neighbourhood, coordinates, URL, cover; detail: description, all photos, address, province, publicRef, characteristics | `search_listings` до 10 за ответ, `more_results` до 10 страниц; сервер сообщил 12,223 Montevideo apartments for sale; response headers reported 300 requests/minute | Выбран для MVP |
| Gallito official REST API | REST v1 | Требует регистрации приложения и User Key; планы ограничивают дневные/минутные вызовы | Документация описывает поиск объявлений и полные карточки, включая поля объектов и изображения | Документация указывает `$skip/$top/$orderby`, лимиты зависят от плана; Basic page сообщает 4,000 hits/day и отдельные limits | Резервный вариант после получения ключа |
| Mercado Libre Uruguay | Mercado Libre Items API | OAuth 2.0 и зарегистрированное приложение; API предназначена прежде всего для seller/integrator flows | Категория Inmuebles, item price/currency, location, pictures, attributes | API и функциональные scopes зависят от приложения; публичный anonymous search/storage regime для регулярного каталога не подтверждён | Не выбран для текущего MVP |

## MCP handshake и разрешённый режим

Проверен настоящий Streamable HTTP MCP protocol, а не обычный GET:

1. `POST /api/public-mcp`, JSON-RPC `initialize` с protocol version
   `2025-03-26` — успешно.
2. `tools/list` — успешно; сервер вернул `search_listings`, `more_results`,
   `get_listing` и другие инструменты.
3. `search_listings` и `get_listing` имеют security scheme `noauth` и
   `readOnlyHint=true`.
4. `search_listings` по Montevideo/apartment/sale вернул 10 live listings из
   `total=12223`, `totalPages=1223`.
5. `get_listing` для первого результата вернул 8 ordered photos и detail
   fields.

Страница MCP явно говорит, что поиск и просмотр объявлений не требуют входа.
`/mcp` перенаправляет на Gallito-branded страницу; API endpoint остаётся
`/api/public-mcp`. Логин, cookies, OAuth, contact options и персональные данные
в адаптере не используются.

## Хранение и ограничения

Публичное MCP-описание разрешает клиентам искать и просматривать объявления, но
не задаёт в найденной документации отдельный срок хранения копий или фотографий.
Поэтому адаптер хранит только нормализованные данные, исходный URL и внешние URL
фотографий; изображения не скачиваются. Долговременное архивирование цен и
фотографий требует отдельного подтверждения условий El País/Gallito.

MCP использует ранжирование портала, а не стабильную выгрузку всего каталога.
`more_results` ограничен 10 страницами (до 100 результатов на один search),
поэтому регулярный импорт должен использовать узкие фильтры и не является
полным экспортом 12,223 записей. Удаление из одного обхода не трактуется как
продажа объекта.

В live MCP response присутствовали `ratelimit: limit=300, remaining=299,
reset=60` и указание, что каждый assistant search расходует daily quota. Эти
значения не заменяют письменное разрешение на фоновый массовый импорт.

## Fixtures

- `testdata/fixtures/gallito/search_montevideo_sale_apartment.json` — сохранённый
  live search response, 10 результатов.
- `testdata/fixtures/gallito/detail_6aeff39935abd95ffb2166d8.json` — сохранённый
  live detail response, 8 фотографий.

Официальные страницы: [MCP для desarrolladores](https://inmuebles.elpais.com.uy/mcp),
[Gallito REST API](https://developer.gallito.com.uy/),
[API plans](https://developer.gallito.com.uy/planes),
[Mercado Libre real estate API](https://developers.mercadolibre.com.uy/es_ar/locate-property/categories-and-attributes).

## Фактический ограниченный импорт

Перед записью проверено:

```sql
SELECT current_database(), current_user;
-- realestate_db | realestate_user
```

Команды dry-run и live-import использовали `city=Montevideo`,
`operation=sale`, `property=apartment`, `limit=10`.

| Проверка | Результат |
|---|---:|
| Live search records received | 10 |
| First import created | 10 |
| First import snapshots | 10 |
| Repeat import created | 0 |
| Repeat import updated | 10 |
| Repeat import snapshots | 0 |
| Stored rows | 10 |
| Distinct external IDs | 10 |
| Duplicate `(source_id, external_id)` | 0 |
| Currency | 10 USD |
| Price range | 132,000–209,000 USD |
| Coordinates | 10/10 present; longitude -56.1901091…-56.151241, latitude -34.9038094…-34.88638 |

Запись выполнена только в `realestate_db`; импорт в другие базы не выполнялся.
Фотографии не скачивались, сохранялись только внешние URL. Первый detail API
вызов проверен отдельно для одного live listing и вернул 8 фотографий; массовое
обогащение не запускалось.
