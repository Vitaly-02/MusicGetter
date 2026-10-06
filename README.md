# MusicGetter

Monorepo для переноса музыкальных коллекций из Spotify, Яндекс Музыки и VK Музыки
через browser extension в музыкальный Telegram destination. Лицензия MIT.

Реализован bootstrap Go backend: environment configuration, PostgreSQL pool,
SQL migrations, HTTP health endpoints, JSON slog, request ID, recovery и graceful
shutdown. Добавлены domain model, PostgreSQL repositories и schema version 6. Работают управляющий Telegram bot, pairing/extension sessions
и HTTP API для приёма импорта чанками.
Добавлена MV3 extension foundation: popup, pairing и durable demo outbox.
Исполнение импорта и музыкальный destination пока не реализованы. Yandex DOM adapter
проверен на синтетических fixtures; его подключение к popup/outbox ещё впереди.
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

## Telegram bot

Создайте собственного управляющего бота через BotFather и добавьте
`TELEGRAM_BOT_TOKEN` в локальный `.env` (файл игнорируется Git). Токен музыкального
destination не нужен. Для запуска на хосте после настройки PostgreSQL:

```sh
set -a
. ./.env
set +a
make migrate-up
make bot
```

В другом терминале `make run` запускает HTTP endpoint для extension pairing.
Бот работает в long polling: публичный Telegram webhook не нужен, но исходящий
HTTPS к api.telegram.org обязателен. Для одного bot token запускайте один poller.
Если ранее настроен webhook, отключите его отдельно перед запуском. SIGINT/SIGTERM
завершает long poll. Ошибки авторизации/конфликт poller приводят к остановке.

Запуск через Docker (после заполнения `.env`):

```sh
make docker-up
docker compose --profile bot up -d --build bot
docker compose logs -f bot
# Остановить все сервисы, включая opt-in bot:
docker compose --profile bot down
```

Бот не запускается по умолчанию при `make docker-up`.

| Команда бота | Поведение |
|---|---|
| `/start`, `/help` | Описание, справка и inline menu |
| `/connect` | Одноразовый код на 5 минут; новый код отменяет предыдущий |
| `/imports` | Импорты пользователя, страницы по 10 записей |
| `/status [UUID]` | Состояние и счётчики треков; без ID — последний импорт |
| `/playlists` | Сохранённые destination collections; без обращения к музыкальному боту |
| `/settings` | Число активных sessions и кнопка отзыва всех подключений |

Команды и callbacks работают только в личном чате владельца. Код из `/connect`
вводится в расширение; backend принимает `POST /v1/pair/claim` с JSON
`{"code":"..."}` и возвращает собственный bearer token один раз. Сессия действует
30 дней; проверка через `GET /v1/me` с Authorization: Bearer.
В БД хранятся только SHA-256 hashes. Отзыв из settings закрывает sessions и pending
codes. Подробные DTO и статусы — [протокол](docs/protocol.md).

Повтор Telegram update не запускает команду снова. Если процесс упал после claim,
ответ может потеряться: повторите команду новым сообщением. При потерянном ответе
redeem запросите новый `/connect`. Импорт pipeline и управление удалёнными
плейлистами на этом этапе не запускаются.

## Команды

| Команда | Что делает |
|---|---|
| `make bot` | Собрать bin/bot и запустить long polling |
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
БД/несовместимой схеме, domain constraints, concurrent deduplication, pairing,
job leases/fencing и rollback составных операций. Lifecycle tests используют настоящие локальные TCP listeners.

## Domain и БД

Модели: User, ExtensionPairing, SourceCollection, Import, ImportItem, CanonicalTrack,
DestinationTrack, TrackMapping, DestinationCollection, DestinationMembership,
ImportJob; SourceProfile/DestinationConnection изолируют аккаунты. Source selection
поддерживается отдельно от destination kinds.

Fingerprint v1 вычисляется из нормализованных metadata при отсутствии source key.
Уникальные ограничения блокируют повторный item и membership, в том числе при
конкурентных вставках. Reserved membership не подтверждает удалённую отправку;
worker и destination integration ещё не реализованы.

[Схема, ограничения, repositories и границы fingerprint](docs/database.md).
Применить новые миграции локально: `make migrate-up`; обновить Docker backend и
его встроенные SQL migrations: `make docker-up`. Откат domain migrations удаляет
данные и требует backup; integration tests выполняют его только в отдельной БД.

## API расширения

[OpenAPI 3.1.1](docs/openapi.json) · [Запросы и сценарии retry](docs/extension-api.md).

Реализованы `POST /v1/pair/claim`, `GET /v1/me`, `GET /v1/destinations`,
`POST /v1/imports`, `POST /v1/imports/{id}/tracks`, `/complete`, `/cancel`
и `GET /v1/imports/{id}`. Все, кроме claim, требуют собственного Bearer token.

Чанки: 1–200 tracks, максимум 512 KiB. `client_request_id` дедуплицирует создание,
`idempotency_key` + sequence + server digest — повтор чанка. Complete проверяет
непрерывность чанков и создаёт jobs; worker пока не исполняется. До complete import
остаётся collecting. GET status возвращает счётчики, без выгрузки всей библиотеки.

В `.env` задайте `EXTENSION_ORIGINS` точными origins установленных расширений.
Пустой список запрещает все запросы с Origin; без Origin запросы допустимы при
Bearer authentication. Cookies/source credentials/unknown JSON fields отклоняются.
Лимиты по IP, owner и claim — на процесс; 429 возвращает Retry-After. За proxy
X-Forwarded-For не доверяется. Production transport — HTTPS.

`GET /v1/destinations` читает сохранённые пользовательские collections. У нового
пользователя список пуст: реальный destination/provisioning ещё не подключён.
Миграции и обновление Docker: `make docker-up`. Спецификацию дополнительно можно
проверить `uvx --from openapi-spec-validator openapi-spec-validator docs/openapi.json`.

## Browser extension

```sh
cd extension
npm ci
npm run typecheck
npm test
npm run build
```

В `chrome://extensions` включите режим разработчика и загрузите распакованную
папку `extension/dist/chrome`. Backend по умолчанию `http://127.0.0.1:8080`;
другой HTTPS origin задаётся `MUSICGETTER_BACKEND_URL` при build. Укажите origin
установленного расширения в `EXTENSION_ORIGINS` backend, получите `/connect` в
боте и введите код в popup. Token хранится в private extension-origin IndexedDB.

Spotify/VK остаются stubs. Yandex DOM adapter поддерживает favorites/playlist/album
на fixture-разметке, реальный сбор ещё не подключён к popup. Opt-in demo отправляет 450
синтетических треков в выбранную существующую destination collection (не создаёт
destination автоматически). Работают replay неизменных chunks, backoff,
progress и cancel. Реальный DOM capture — следующий этап. Отдельная сборка
`npm run build:firefox` использует event background; browser smoke пока ручной.

[Установка, приватность токена и recovery](extension/README.md).

## Конфигурация

| Environment variable | Default | Назначение |
|---|---|---|
| TELEGRAM_BOT_TOKEN | обязательно для bot | Токен нашего управляющего бота из BotFather |
| DATABASE_URL | обязательно | PostgreSQL URL/DSN; production TLS настраивается здесь |
| EXTENSION_ORIGINS | пусто | Exact comma-separated chrome-extension/moz-extension origins для CORS |
| API_IP_PER_MINUTE / API_OWNER_PER_MINUTE / API_CLAIM_PER_MINUTE | 120 / 60 / 5 | Fixed-minute rate limits на процесс, диапазон 1..10000 |
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
  а не authentication. Bearer authorization реализована для всех endpoints расширения, кроме claim.
- Логи содержат status/duration/request ID; URL query, DSN и panic values исключены.
  Recovery до отправки ответа возвращает JSON 500; после частичного ответа
  прерывает connection/stream, не дописывая JSON к успешному payload.

## Документация

- [Архитектура и структура](docs/architecture.md)
- [Сущности и ограничения БД](docs/database.md)
- [Миграции](migrations/README.md)
- [Протокол приёма и привязка](docs/protocol.md)
- [Extension API и retry](docs/extension-api.md)
- [OpenAPI](docs/openapi.json)
- [Проверки](docs/verification.md)
- [ADR](docs/adr/README.md)
- [Правила разработки](AGENTS.md)

Module path `musicgetter` пока локальный; перед публикацией заменить на адрес repo.
