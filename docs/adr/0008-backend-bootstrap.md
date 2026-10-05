# ADR-0008: Backend bootstrap, миграции и lifecycle

Статус: принято. Дата: 2026-10-05. Уточняет ADR-0002 и ADR-0007.

## Контекст

Нужен исполняемый backend без business logic. Конфигурация должна валидироваться,
ошибки не должны раскрывать secrets, migrations отделены от server startup.

## Решение

- Go 1.27.1, net/http и JSON slog. Environment config валидируется при старте;
  `.env` не читается backend автоматически, example содержит только dev значения.
- pgxpool проверяет соединение при открытии; pool закрывается после HTTP shutdown.
- Goose v3 Provider, pgx stdlib только для runner, embedded SQL, transactional
  migrations и PostgreSQL advisory session lock. Первая migration создаёт namespace,
  не domain tables. Отдельный `cmd/migrate up|down`, migration timeout.
- Liveness не зависит от БД. Readiness проверяет схему query с deadline и возвращает
  503 при draining. Требуется точное совпадение expected version (пока без окна
  совместимости для rolling schema deployment).
- Request middleware передаёт context, deadline, validated/generated request ID
  и logger. JSON errors не содержат внутренних причин. Partial-response panic
  завершает соединение через ErrAbortHandler; panic values не логируются.
- Shutdown сначала запрещает readiness и перестаёт принимать подключения; запросы
  продолжают работать до лимита. Затем context cancellation и принудительный close.
  Downstream должен соблюдать context; произвольную Go goroutine остановить нельзя.
- Compose для local dev: PostgreSQL 17 и backend без root. `make docker-up` запускает
  migrations одноразовым процессом перед server. `docker-down` сохраняет volume.

## Альтернативы и последствия

Самописный migration engine увеличил бы объём собственной логики lock/history/
rollback; Goose добавляет одну прямую dependency. Полный goose CLI с драйверами
всех БД не нужен. sqlc и domain migrations остаются отложенными.

Readiness требует не только TCP/Ping, иначе пустая БД выглядела бы готовой. Exact
schema version пока запрещает смешанные версии backend — перед rolling deploy
нужно определить expand/contract compatibility. Auto-migrate в каждом replica
отвергнут: отдельная команда даёт контролируемый release step.

HTTP_REQUEST_TIMEOUT — cooperative deadline, не убийство handler goroutine.
HTTP WriteTimeout ограничивает сетевую запись. Error logs исключают raw DSN/SQL,
для подробных migration ошибок используются локальные PostgreSQL logs.
Goose history и schema version проверяются integration tests; бизнес-гарантии
идемпотентности пока не реализованы.

## Источники

- [Go releases](https://go.dev/dl/)
- [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool)
- [Goose Provider и session locking](https://pressly.github.io/goose/documentation/provider/)
