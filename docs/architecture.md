# Архитектура MusicGetter

Статус: принятые границы и проект контрактов; реализованы bootstrap backend, domain model, PostgreSQL repositories, управляющий
Telegram bot и собственные extension sessions.
Импорт и интеграции остаются проектом. Детали bootstrap — ADR-0008 и README, persistence — ADR-0009 и docs/database.md, Telegram/auth — ADR-0010.

## Поток данных

```mermaid
flowchart TD
    S[Spotify / Yandex / VK: rendered DOM] --> E[Extension: source adapter]
    E --> O[Extension: durable bounded outbox]
    O -->|HTTPS + собственная авторизация| A[Go HTTP API]
    A --> P[(PostgreSQL: captures, batches, imports, jobs)]
    P --> W[Go import worker]
    W --> M[Matcher + destination catalog]
    M --> D[Destination adapter]
    D --> B[Music Telegram bot / supported integration]
    T[Управляющий Telegram bot] --> U[Application use cases]
    U --> P
```

Ни сервер, ни worker не обращаются к Spotify/VK/Yandex. Source существует только
на стороне расширения; backend принимает нормализованные наблюдения. Поиск
идёт в каталоге destination через разрешённую им интеграцию, без обращения к
стримингам. Конкретная интеграция пока не выбрана и не считается доступной.

## Monorepo и зависимости

```text
.
├── AGENTS.md
├── README.md
├── LICENSE
├── go.mod / go.sum
├── Makefile
├── compose.yaml
├── .env.example
├── cmd/
│   ├── server/                 # рабочий bootstrap HTTP API
│   ├── migrate/                # отдельный migration runner
│   ├── bot/                    # управляющий Telegram bot
│   └── worker/                 # отдельный процесс import jobs
├── internal/
│   ├── app/                    # wiring и lifecycle сервера
│   ├── config/                 # environment configuration
│   ├── logging/                # JSON slog, context logger
│   ├── domain/                 # entities, states, metadata, fingerprint v1
│   ├── import/contracts.go     # ingestion, leases; package importer
│   ├── pairing/                # issue/redeem/revoke собственных credentials
│   ├── matcher/contracts.go    # catalog и versioned matching policy
│   ├── destination/contracts.go
│   ├── storage/postgres/       # pool, readiness, Goose и domain repositories
│   ├── api/                    # transport, auth, DTO validation
│   └── telegram/               # handlers управляющего бота
├── extension/
│   ├── README.md
│   ├── tsconfig.json
│   └── src/
│       ├── core/contracts.ts
│       └── sources/
│           ├── spotify/
│           ├── yandex/
│           └── vk/
├── migrations/                # embedded Goose SQL + README
├── docs/
│   ├── architecture.md
│   ├── database.md
│   ├── protocol.md
│   ├── verification.md
│   └── adr/                    # 0001–0010 + индекс
└── deploy/Dockerfile           # server, migrate и bot, runtime без root
```

Пустые каталоги закреплены .gitkeep. cmd/worker добавлен, чтобы масштабирование
и перезапуск HTTP/bot процессов не зависели от длительных импортов. Это один
модульный backend с общей БД, без распределённых внутренних RPC. В разработке
возможен один процесс, но orchestration не связывается с HTTP handler lifetime.

Зависимости: transport → application (import, matcher) → domain; storage и
конкретные destination adapters реализуют узкие порты application. Wiring только
в cmd. Domain не импортирует infrastructure. `import` — имя каталога, `importer` —
имя Go package (import является ключевым словом).

Go 1.27.1, Go modules, net/http, slog, pgxpool и Goose Provider для миграций.
Прямые зависимости — pgx, Goose и golang.org/x/text для Unicode normalization;
sqlc пока не добавлен, SQL repositories небольшие и явные.
TypeScript toolchain/lockfile и MV3 manifest появятся с первой реализацией extension.

## Извлечение из DOM

Каждый adapter сам определяет поддержанные страницы, разбирает видимые карточки,
строки и разрешённые ссылки. Общий core отвечает только за нормализованную схему,
очередь, транспорт и lifecycle. Selectors и смысл DOM не делятся между сервисами.

Виртуализированный список нельзя выгрузить одним querySelectorAll. Adapter
наблюдает уже отрендеренные строки при пользовательской прокрутке, сохраняет
пакеты постепенно и не удерживает DOM nodes. Помощь с прокруткой допустима только
в явно запущенном режиме с остановкой; она не должна вызывать скрытые запросы
самостоятельно или обходить доступ. Наличие DOM-элемента само по себе не означает
видимость: скрытые stores/scripts и неотображаемые payloads исключены.

Стабильный source key берётся из разрешённой видимой ссылки. URL разбирается
локально: сохраняется только разрешённый идентификатор/путь без query и fragment.
Если такой ссылки нет — provisional key из metadata с явной неопределённостью.
Нельзя объединять разные записи только по title/artist. Одинаковые наблюдения
можно сжать внутри capture, но fingerprint не доказывает тождественность аудиозаписи. По ADR-0009 canonical
metadata identity переиспользуется внутри profile между imports; provisional
mapping требует повторной проверки перед автоматическим принятием совпадения.

Каждый capture относится к одной коллекции и выбранному source profile. Полный
импорт библиотеки — несколько captures коллекций, а не один огромный payload.
Profile задаётся пользователем без чтения streaming credentials. Пользователь
подтверждает profile при смене аккаунта в браузере; автоматического определения
реального streaming account и доказательства владения через DOM нет.

`complete` допускается лишь при проверяемом видимом конце и согласованном видимом
счётчике, если он есть. Тишина MutationObserver/таймаут — не доказательство полноты.
`partial` сохраняется и может импортироваться. Ни один capture не удаляет треки
из destination, отсутствующие в наблюдённой странице.

## Pipeline и состояния

1. Привязать собственную extension session к Telegram user, выбрать source profile
   и destination connection/target. Проверить capabilities до запуска.
2. Создать capture. Принимать bounded batches; сохранять ACK только после commit.
3. Seal проверяет непрерывность sequences и в одной транзакции создаёт import,
   items и начальные jobs. Незавершённый capture не выполняет внешние эффекты.
4. Worker claim-ит небольшой диапазон работы, нормализует metadata, запрашивает
   bounded candidates в destination catalog, применяет versioned matcher.
5. Уверенный результат закрепляет destination track ID. Неоднозначный/пустой
   результат отправляет item в needs_review; управляющий бот показывает варианты,
   позволяет выбрать, пропустить или повторить поиск после изменения metadata.
6. Для каждой membership worker атомарно резервирует operation и выполняет
   EnsureTrack. applied/already_present закрывают item; pending/unknown запускают
   reconciliation; rejected становится ошибкой или управляемым retry.
7. Прогресс читается из БД; Telegram updates агрегируются с ограничением частоты.

Capture: collecting → sealed_partial | sealed_complete | aborted. Seal неизменяем.
Import: queued → running → completed | completed_with_errors | needs_attention |
failed | cancelled. После решения всех review cases needs_attention → running;
если есть параллельная полезная работа, import остаётся running. Failed означает
неустранимую ошибку всего импорта; локальные ошибки дают completed_with_errors.
Item: pending → searching → matched → ensuring → added | already_present;
searching → needs_review → matched | skipped; ensuring → reconciling → ensuring
(только если безопасно) | added | already_present | needs_review | failed.
Любой ещё не отправленный item может быть cancelled. Job и item — разные сущности:
несколько попыток job не увеличивают число обработанных треков.

Cancel прекращает новые внешние эффекты и jobs, но не откатывает уже добавленное.
Отправленное до отмены доводится до known outcome/review; неизвестные операции
продолжают reconciliation даже для cancelled import. Retry не сбрасывает ledger.
Total фиксируется после seal. Progress — взаимоисключающие buckets текущих items;
matched включает ожидание ensure/reconcile, pending — поиск, needs_review включает
неоднозначный match/unknown effect. Для skipped хранится отдельный счётчик.

## Основные порты

- `SourceAdapter`: supports, inspect, capture (async bounded generator).
- `Ingestion`: Append, Seal; валидирует владельца и immutable batch replay.
- `JobQueue`: Claim, Renew, Complete, Retry, Fail; все изменения fenced lease.
- `Catalog`: Search; предоставляет destination integration.
- `Matcher`: Match; policy version, ranked candidates, no silent ambiguous choice.
- `Destination`: Capabilities, EnsureTrack; связь с конкретным ботом скрыта.
- Опциональные `MembershipReader`, `OperationReconciler`, `TargetManager`.

Контракты приведены в .go/.ts; это compile-time границы, не готовый wire protocol.
Transport DTO не сериализуются напрямую из domain structs. DB repositories реализованы отдельно по ролям и принимают pgxpool или pgx.Tx;
универсального CRUD Store нет. Импортный HTTP flow пока не подключён.

## Идемпотентность и внешние эффекты

Идемпотентность действует на нескольких независимых уровнях:
- create/seal capture: собственный request key, replay возвращает тот же ресурс;
- batch: (capture, sequence) + server-calculated canonical digest, immutable replay;
- jobs: уникальный logical work key, lease generation;
- destination membership: (connection, target, destination_track_id);
- target creation: сохранённое mapping и стабильный operation key.

Source track и destination track не эквивалентны. Два исходных трека могут совпасть
с одним destination ID. UNIQUE membership относится к конечному ID и target,
поэтому protects favorites/playlist/album и между импортами, и между source adapters.
Дубликат в другом target допустим. В первой версии внутри target — set semantics:
повторные вхождения одного трека в исходном playlist не воспроизводятся.

Ledger знает только наблюдавшиеся системой операции. Чтобы не дублировать ранее
добавленные пользователем треки, adapter должен поддержать atomic ensure либо
надёжное чтение существующей membership. Native request idempotency сама по себе
не обнаруживает трек, ранее добавленный другим способом. Read-before-add безопасен
от наших конкурентных writes только при сериализации target; при внешних writers
без atomic ensure строгая гарантия невозможна. В строгом режиме такие destinations
не допускаются, пока не доказано исключительное владение записью и reconciliation.

После timeout возможен успешный внешний effect. Нельзя просто повторить send.
Повтор разрешён только с native idempotency/atomic ensure либо после достоверного
определения результата. ReadMembership=false + нет безопасного ensure/reconciliation
означает unsupported capability, а не ослабление требований пользователя.
Database fencing предотвращает устаревший commit, но не отменяет внешний запрос.

## Масштабирование и эксплуатация

Стартовые проектные лимиты: до 200 items и 512 KiB JSON на batch, до 4 batches
in flight, bounded outbox до 20 MiB с паузой при переполнении, keyset pages до 200.
Это настройки, а не обещания измеренной производительности. При 50 000 треков
очередь хранится в БД; клиент хранит только неподтверждённые batches и compact keys.
Контрольные задачи на диапазоны/items, не один job на всю библиотеку и не
неограниченная fan-out загрузка. Индексы и нагрузочные критерии — в database/verification.

Job claim использует короткую транзакцию SKIP LOCKED. Lease heartbeat, generation,
exponential backoff + jitter, destination-specific rate limit и max attempts.
Crash между DB commit и запуском worker покрывает durable jobs. Crash вокруг
внешнего effect покрывают ledger и reconciliation, не транзакция PostgreSQL.

Метрики: backlog/oldest job age, retries, capture partial rate, match ambiguity,
unknown effects, latency и rate limiting. slog пишет correlation IDs и safe error
codes; названия треков, коллекций, Telegram сообщения и секреты по умолчанию исключены.
TLS снаружи; migrations выполняются отдельным release step. Go/DB timeouts,
graceful shutdown с прекращением claim и завершением/освобождением leases.
Сроки хранения, резервные копии и удаление пользователя уточняются до production.

## Открытые вопросы

- Какой музыкальный destination и какой разрешённый протокол реально доступны?
  Работа обычного Telegram Bot API сама по себе не доказывает bot-to-bot delivery.
  Автоматизацию пользовательского аккаунта не вводим по умолчанию.
- Есть ли atomic ensure, поиск, чтение membership, receipts и поддержка albums?
  Без них часть требований может оказаться невыполнимой для выбранного бота.
- Album: в проекте это source collection → destination target; если destination
  не поддерживает album, предложить явно согласованный playlist или отказ.
- Set semantics теряет повторные позиции playlist; порядок первой версии — best
  effort. Нужны ли повторы/точный порядок — отдельное продуктовое решение.
- DOM selectors и признаки полного обхода ещё нужно проверить на реальных страницах.
- Пороги matcher, длительности leases, retention/quotas, hosting, поддерживаемые
  версии browser требуют реализации и измерений. Bootstrap использует Go 1.27.1
  и PostgreSQL 17 в Compose; integration suite также проверен на локальном PostgreSQL 14.
