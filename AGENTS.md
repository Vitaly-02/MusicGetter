# Правила MusicGetter

## Область действия и стадия
Эти правила применяются ко всему monorepo. Перед работой прочитайте
`docs/architecture.md`, `docs/database.md`, `docs/verification.md` и ADR в
`docs/adr/`. Реализованы bootstrap HTTP backend, domain model, PostgreSQL
repositories, отдельная команда SQL migrations, управляющий Telegram bot
и bot-issued pairing/extension sessions (ADR-0010), HTTP API приёма импорта
чанками (ADR-0011, docs/openapi.json), MV3 extension foundation с popup, pairing
и durable outbox (ADR-0012), Import all/Select tracks с DOM selection (ADR-0016). Исполнение импорта, worker
и реальные destination adapters ещё не реализованы. Yandex, Spotify и VK DOM adapters реализованы
и проверены на синтетических fixtures (ADR-0013/0014/0015).
Не выдавайте проектируемые возможности за работающие. Пользователь определяет
границы очередной задачи; изменение принятого решения отражайте в ADR.

## Непересекаемые границы
- Backend, worker и управляющий Telegram bot — Go, один Go module.
- Browser extension — TypeScript, Manifest V3.
- Spotify, Yandex Music, VK Music: только чтение данных, уже отрендеренных
  и доступных пользователю в странице. Не обращаться к официальным,
  unofficial/private API, не исследовать внутренние HTTP endpoint'ы.
- Не перехватывать, повторять или анализировать XHR/fetch для извлечения
  музыкальной библиотеки. Не читать hydration stores, внутреннее состояние
  приложения, скрытые JSON payloads, cookies, localStorage/sessionStorage
  или токены стриминга. Не обходить ограничения доступа.
- Допускается пользовательское раскрытие разделов и прокрутка; не обещать
  экспорт не загруженной страницы. Частичный сбор обозначать явно.
- На backend отправлять только разрешённые поля DTO. Не отправлять cookies,
  auth headers, credentials стримингов, HTML, скриншоты или произвольные URL.
- Собственная авторизация MusicGetter отделена от авторизации стримингов.
- Source adapters полностью изолированы: не импортируют друг друга, общие
  типы и DOM utilities находятся в extension/src/core.
- Конкретный музыкальный бот скрыт за Destination. Управляющий бот проекта
  и музыкальный destination — разные роли. Не предполагать, что произвольные
  боты умеют взаимодействовать друг с другом.

## Архитектура и данные
- Domain не зависит от HTTP, Telegram SDK, pgx и адаптеров. Не создавать giant
  interfaces/files; интерфейсы определять по роли, обычно рядом с потребителем.
- Использовать PostgreSQL, pgx, SQL migrations; sqlc вводить при наличии
  повторяющихся запросов и реальной пользы. HTTP — net/http, логи — slog.
- Минимум зависимостей. Redis/RabbitMQ пока не добавлять. Очередь — PostgreSQL,
  jobs + SELECT FOR UPDATE SKIP LOCKED, lease, fencing и ограниченные повторы.
- Canonical fingerprint v1 и его limitations описаны в docs/database.md и ADR-0009.
  Не менять normalization незаметно; не считать metadata fingerprint акустическим ID.
- Идемпотентность обязательна на уровнях приёма batch, jobs и membership.
  Повторный импорт не добавляет существующий destination track в тот же target.
- Не объявлять exactly-once внешнего эффекта без доказуемого механизма.
  Неопределённый результат отправки требует reconciliation, не слепого retry.
- Тысячи и десятки тысяч треков: bounded batches, backpressure, keyset pagination,
  лимиты параллелизма; не загружать библиотеку целиком в RAM или одно сообщение.
- Все чтения/изменения привязаны к владельцу. ID пользователя из входного DTO
  не является авторизацией. Не писать credentials и названия коллекций в логи
  по умолчанию. Secrets только из конфигурации окружения/secret storage.
- Изменение схемы — миграция с описанием совместимости, блокировок и отката;
  тесты на реальном PostgreSQL для UNIQUE, транзакций и конкурентного claim.

## Telegram и собственная авторизация
- Команды принимаются только от проверенных private-chat updates через Bot API.
  Owner всегда определяется по from.id; callback/DTO не задаёт owner.
- Pairing: crypto/rand 128 bits, TTL 5 минут; extension token: 256 bits, TTL 30 дней.
  В БД только SHA-256 hashes. Redeem и создание session — одна транзакция.
- Issue/redeem/revoke сериализуются по user row. Revoke отменяет pending codes.
- Не логировать Telegram payloads, pairing codes, bearer tokens и Bot API URLs.
  Обёртки transport errors не должны сохранять URL с bot token.
- Claim update до handler даёт at-most-once попытку, а не гарантированную доставку.
  Новый business use case должен отдельно определить semantics retries.

## Extension API
- API v1 и DTO фиксируются в docs/openapi.json; изменять контракт и tests вместе.
- Strict allowlist JSON, запрет unknown/duplicate fields и credentials, bounded bodies.
- 200 tracks/512 KiB на chunk; 100 000 observations и 10 000 chunks на import.
- collecting import не создаёт jobs до complete с непрерывным диапазоном chunks.
- Receipt key/sequence/digest immutable; replay безопасен после seal/cancel.
- CORS только exact extension origins; Origin не заменяет Bearer authentication.
- Rate limiter bounded, per process; при нескольких replicas нужен общий budget.

## Browser extension
- TypeScript, MV3, Chrome first; Firefox-specific background manifest собирается отдельно.
- Tokens только в extension-origin IndexedDB, никогда в content/page RPC или sync storage.
- Content предоставляет page.info и scoped selection RPC; Yandex/Spotify/VK подключены
  к popup и durable outbox. Selection metadata только в extension-origin IndexedDB,
  identity по source key/normalized metadata digest, не по DOM node.
- Capture RPC привязан к owner/origin/tab/frame/document nonce; tokens не отправляются content.
- Frozen selection immutable; keyset cursor продвигается только после ACK.
  Reload страницы требует нового режима; selection переживает scroll/recycling и worker restart.
  Synthetic DemoProducer не выдаётся за пользовательскую библиотеку.
- MusicSourceAdapter.collectAllTracks — AsyncGenerator bounded batches; не Promise всей библиотеки.
- Pending chunk сохраняется до POST, ACK проверяется до удаления. Worker restart не меняет key/body.
- Смена owner/backend не позволяет replay чужой очереди; logout не восстанавливается late response.
- Extension tests: npm ci, npm run typecheck, npm test, npm run build и build:firefox.

## Проверка изменений
- При реализации важных решений добавлять поведенческие тесты, включая сбои
  и concurrency; матрица в docs/verification.md. Не подменять их тестами заглушек.
- Go: make test, make lint; изменения pool/migrations дополнительно проверять
  make test-integration с TEST_DATABASE_URL локального PostgreSQL.
  TypeScript: npm ci && npm run typecheck
  в extension после появления lockfile/toolchain. Проверять DOM adapters на
  обезличенных rendered-DOM fixtures без сетевых запросов к стримингам.
- Не добавлять основную логику, реальные credentials или рабочую интеграцию
  destination в рамках задачи только на архитектуру.
