# MusicGetter

Monorepo для переноса музыкальных коллекций из Spotify, Яндекс Музыки и VK Музыки
через browser extension в музыкальный Telegram destination. Лицензия MIT.

Реализован bootstrap Go backend: environment configuration, PostgreSQL pool,
SQL migrations, HTTP health endpoints, JSON slog, request ID, recovery и graceful
shutdown. Импорт, Telegram integrations и extension runtime пока не реализованы.
Расширение будет читать только доступный пользователю rendered DOM; API стримингов,
перехват запросов и передача их credentials запрещены.

## Быстрый запуск в Docker

Нужны Docker Engine с доступным сокетом и Docker Compose v2+ с поддержкой `--wait`.
Из корня репозитория:

```sh
cp .env.example .env
make docker-up
curl -i http://localhost:8080/health/live
curl -i http://localhost:8080/health/ready
docker compose logs -f backend
```

`make docker-up` поднимает PostgreSQL 17, ждёт его готовности, собирает image,
выполняет миграции отдельным одноразовым процессом и запускает backend.
`docker compose up` сам по себе миграции НЕ применяет: на чистой БД readiness
останется 503. Контейнер backend работает без root; внутренний порт по умолчанию 8080,
при необходимости его можно изменить через BACKEND_INTERNAL_PORT в `.env`.
TLS для production должен завершаться на доверенном reverse proxy.

```sh
make docker-down
```

Остановка сохраняет volume PostgreSQL. Порты 5432 и 8080 публикуются только на
127.0.0.1. При конфликте задайте `POSTGRES_PORT`/`BACKEND_PORT` в `.env`.
Данные `POSTGRES_USER/PASSWORD/DB` применяются при первой инициализации volume;
их смена в `.env` не изменяет существующую БД. Пароль в URL должен быть URL-encoded,
если содержит специальные символы. Значения example предназначены только для dev.

## Backend на хосте, PostgreSQL в Docker

Нужен Go 1.27.1+, make и C toolchain для `go test -race`. Go 1.21+ с
`GOTOOLCHAIN=auto` скачает требуемый toolchain из go.mod при наличии сети.

```sh
cp .env.example .env  # только при первом запуске
# Отредактируйте .env; при смене POSTGRES_PORT обновите и DATABASE_URL.
set -a
. ./.env
set +a

docker compose up -d --wait postgres
make migrate-up
make run
```

Если backend уже запущен в Docker, сначала выполните `docker compose stop backend`
или задайте другой `HTTP_ADDR`. Для собственного PostgreSQL достаточно указать
`DATABASE_URL` и выполнить две последние команды. Процесс НЕ читает `.env`
автоматически: на хосте нужны exported environment variables. Compose читает `.env`
для подстановок, а DATABASE_URL backend формирует с Docker hostname `postgres`.

Остановить `make run` можно через Ctrl+C; бинарник обрабатывает SIGINT/SIGTERM.
Пул проверяется при старте; недоступная БД или неверная конфигурация дают exit 1.
Пустая/несовместимая схема не мешает запуску HTTP, но readiness возвращает 503.

## Команды

| Команда | Что делает |
|---|---|
| `make run` | Собрать bin/server и запустить в foreground |
| `make test` | Unit и локальные HTTP lifecycle tests с race detector, без PostgreSQL |
| `make lint` | Проверить gofmt и go vet |
| `make migrate-up` | Применить все ожидающие SQL migrations |
| `make migrate-down` | Откатить только одну последнюю migration |
| `make generate` | Выполнить go generate ./...; генераторов пока нет |
| `make docker-up` | PostgreSQL → одноразовые миграции → backend |
| `make docker-down` | Остановить Compose, сохранить DB volume |
| `make test-integration` | Дополнительные тесты на настоящем PostgreSQL |

В Docker миграцию можно вызвать отдельно:
`docker compose run --rm --no-deps backend /app/migrate up`.
Откат: та же команда с `down`. Учитывайте совместимость работающего backend
со схемой. Bootstrap down удаляет только пустую schema `musicgetter`, без CASCADE.

## Тесты с PostgreSQL

```sh
make test
make lint
# После запуска локального PostgreSQL и экспорта DATABASE_URL:
export TEST_DATABASE_URL="$DATABASE_URL"
make test-integration
```

Тестовая роль должна иметь CREATEDB. Тесты создают отдельные случайно именованные
БД `musicgetter_test_*`, после завершения удаляют только их. Указанная в URL БД
используется как административное подключение; её схема не изменяется. Не задавайте
production URL. Без TEST_DATABASE_URL команда integration завершается ошибкой,
а не молча пропускает проверки. Проверяются migrations up/replay/down, конкурентный
migration lock, SQL rollback, pool и отмена запросов, readiness при недоступности
БД/несовместимой схеме. Lifecycle tests используют настоящие локальные TCP listeners.

## Конфигурация

| Environment variable | Default | Назначение |
|---|---|---|
| DATABASE_URL | обязательно | PostgreSQL URL/DSN; production TLS настраивается здесь |
| HTTP_ADDR | :8080 | Адрес listener на хосте |
| LOG_LEVEL | info | debug/info/warn/error, JSON в stdout |
| DB_MAX_CONNS / DB_MIN_CONNS | 10 / 0 | Лимиты пула, min <= max |
| DB_CONNECT_TIMEOUT | 5s | Подключение и startup check |
| DB_MAX_CONN_LIFETIME / DB_MAX_CONN_IDLE_TIME | 1h / 5m | Время жизни/простоя соединения |
| HTTP_READ_HEADER_TIMEOUT / HTTP_READ_TIMEOUT | 5s / 15s | Чтение HTTP headers / request |
| HTTP_WRITE_TIMEOUT / HTTP_IDLE_TIMEOUT | 15s / 60s | Запись ответа / keep-alive |
| HTTP_REQUEST_TIMEOUT | 10s | Deadline request context, downstream обязан его соблюдать |
| HEALTH_TIMEOUT | 2s | Ограничение readiness query |
| SHUTDOWN_TIMEOUT | 10s | Завершение активных HTTP requests |
| MIGRATION_TIMEOUT | 60s | Общий deadline migration command, включая lock |

Все durations положительные; `HEALTH_TIMEOUT <= HTTP_REQUEST_TIMEOUT <
HTTP_WRITE_TIMEOUT`. Compose ждёт остановку 30s: при увеличении SHUTDOWN_TIMEOUT
увеличьте также `stop_grace_period`. Базовый request context сохраняет values,
но не отменяется сразу по SIGTERM: сначала завершение активных запросов, затем
при истечении лимита cancel + закрытие соединений; pool закрывается последним.

## HTTP и логи

- `GET /health/live`: 200 без обращения к БД.
- `GET /health/ready`: 200 только при доступной БД и ожидаемой версии schema;
  503 при сбое, отсутствующей migration или draining.
- Неизвестный маршрут: JSON 404; неподдержанный метод health endpoint: JSON 405.
- Ошибка: `{"error":{"code":"not_ready","message":"Service is not ready"},"requestId":"..."}`.
- X-Request-ID принимается только из 1–64 ASCII букв/цифр/`-`/`_`, иначе генерируется.
  Он передаётся в response header, context и request logger; это correlation,
  а не authentication. Авторизация бизнес-API ещё не реализована.
- Логи содержат status/duration/request ID; URL query, DSN и panic values исключены.
  Recovery до отправки ответа возвращает JSON 500; после частичного ответа
  прерывает connection/stream, не дописывая JSON к успешному payload.

## Документация

- [Архитектура и структура](docs/architecture.md)
- [Сущности и ограничения БД](docs/database.md)
- [Миграции](migrations/README.md)
- [Протокол приёма и привязка](docs/protocol.md)
- [Проверки](docs/verification.md)
- [ADR](docs/adr/README.md)
- [Правила разработки](AGENTS.md)

Module path `musicgetter` пока локальный; перед публикацией заменить на адрес repo.
