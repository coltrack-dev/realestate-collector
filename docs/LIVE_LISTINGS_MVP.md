# Live Listings MVP: VIXORA AI and D.C Colombia

Дата проверки: 2026-11-08

## Статус сети

Официальные страницы документации описывают публичные API. Первоначально
обычный sandbox блокировал сеть, но после on-request разрешения read-only
HTTPS-запросы были выполнены через публичные endpoints.

```text
https_proxy=http://127.0.0.1:8118
curl -> Failed to connect to 127.0.0.1 port 8118
direct -> Could not resolve host
socks5h://192.168.13.128:4712 -> Failed to connect
```

После разрешения сохранены реальные ответы:

- `testdata/fixtures/vixora/live_page_1.json` — 30 опубликованных объектов,
  API сообщил `total=30`.
- `testdata/fixtures/dccolombia/live_properties.json` — реальный JSON-каталог,
  из которого для импорта ограничены первые 50 объектов.
- `testdata/fixtures/dccolombia/live_properties.csv` — реальный CSV-ответ
  endpoint `/inmuebles.csv`.

Fixtures `documentation_example` остаются отдельными и не используются как
статистика live.

## VIXORA AI

Официальная документация описывает `GET
https://api.vixoraai.com/public/v1/properties` как публичный JSON API только
для чтения, без ключа и регистрации. Доступны фильтры типа, операции,
муниципалитета, цены, площади и комнат; пагинация — `page`/`page_size`, максимум
50. Ответ содержит `code`, канонический URL, цену COP, локацию, округлённые
координаты, площади, комнаты, изображения и timestamps.

Источник: [VIXORA API и правила использования](https://vixoraai.com/desarrolladores)
и [OpenAPI 3.1](https://api.vixoraai.com/public/v1/openapi.json).

Фактический ответ от 2026-11-08 содержал 30 записей: 29 продаж и 1 аренду;
импортированы все 30.

Ограничения: API указывает rate limit; в разных разделах документации указаны
60 и 120 запросов в минуту, поэтому адаптер использует консервативную
последовательную задержку 3 секунды. Правила разрешают показывать/цитировать
объект с кодом и ссылкой, но рекомендуют не хранить копии цен долго и
запрашивать API для актуальной информации. Бессрочное накопление price
snapshots без отдельного согласования не включается в эксплуатационный режим.

## D.C Colombia

Официальная документация описывает `GET /api/properties` как публичный JSON
endpoint с активным инвентарём и максимумом 200 результатов, а также
`GET /inmuebles.csv`. Поля включают `slug`, название, операцию, тип, цены
продажи/аренды, площадь, комнаты, ванные, парковку, город, район, координаты и
обложку. `slug` используется как `external_id`; canonical URL строится как
`/inmueble/{slug}`, если API не прислал URL.

Источник: [D.C Colombia API docs](https://www.dccolombia.com.co/docs) и
[условия сайта](https://www.dccolombia.com.co/terminos).

Документация говорит, что inventory синхронизируется из Wasi и просит
связываться с компанией для интеграций большого объёма. Поэтому адаптер
ограничен одним последовательным запросом до 200 объектов и не запускается
планировщиком автоматически. Контактные данные из lead/API в Listing не
переносятся.

Фактический ответ от 2026-11-08 содержал 200 объектов в JSON-каталоге;
импортированы первые 50. CSV endpoint также проверен и сохранён локально.

## Фактический импорт и повторная синхронизация

Перед записью проверено:

```sql
select current_database(), current_user;
-- realestate_db | realestate_user
```

Первый импорт:

| Источник | Прочитано | Создано | Обновлено | Snapshots |
|---|---:|---:|---:|---:|
| vixora | 30 | 30 | 0 | 30 |
| dccolombia | 50 | 50 | 0 | 50 |

Повторный импорт тех же выборок дал `created=0`, `updated=30/50` и
`snapshots_created=0`. SQL-проверка показала 30/30 и 50/50 уникальных
`external_id`, дубликатов нет. Во всех 80 строках цена заполнена; площадь и
координаты заполнены у D.C Colombia, у VIXORA координаты отсутствуют у 16
объектов. В базе осталось по одному snapshot на объект.

## Реализованные адаптеры

- `internal/sources/vixora` — JSON, серверная пагинация, фильтры, ID `code`,
  COP, coordinates/images/timestamps, обработка 429/403 и context cancellation.
- `internal/sources/dccolombia` — JSON, до 200 записей, ID `slug`, sale/rent
  prices, coordinates/images, context cancellation.
- `internal/sources/httpclient` — timeout, User-Agent, последовательный rate
  limit, ограниченные retry только для временных ошибок/429/5xx, `Retry-After`.

Оба адаптера используют существующие `listings`, `listing_snapshots` и
`collection_runs`. Повторный upsert не создаёт дубликатов; одинаковый hash не
создаёт snapshot. Для неполных новых данных действует политика сохранения уже
известных nullable-значений через `COALESCE`, чтобы отсутствие поля не затирало
корректное значение.

## Ручной запуск после восстановления сети

```bash
go run ./cmd/collector -source=vixora -limit=50 -dry-run
go run ./cmd/collector -source=dccolombia -limit=50 -dry-run
```

Для записи в изолированную PostgreSQL-базу убрать `-dry-run` и заранее
проверить `DATABASE_URL`. Live-результат должен быть отдельно записан в журнале
запуска; документационные fixtures не используются как статистика live.

## Регулярный запуск

Подготовлены шаблоны в `deploy/systemd/`. Они не устанавливаются и не
запускаются автоматически. Интервал 24 часа является только начальной
настройкой; включать timer можно после ручной проверки access/storage terms и
первого успешного ограниченного импорта.
