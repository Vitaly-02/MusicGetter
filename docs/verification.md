# Проверки и критерии при реализации

Bootstrap проверяется `make test` (race detector, без внешней БД), `make lint`
и `make test-integration` с TEST_DATABASE_URL. Реализованы проверки environment
validation, JSON logging, request ID, recovery, health errors/deadlines, context
propagation, graceful drain и forced close. Integration suite на реальном PostgreSQL
создаёт изолированные БД и проверяет up/replay/down, concurrent migration lock,
transactional rollback, DROP RESTRICT, pool и query cancellation. Сборка SQL в
бинарник и expected schema version связаны отдельным unit test.

Domain/persistence: golden fingerprint, Unicode/artist-order invariance, сохранение
recording distinctions, missing ID fallback, direct SQL UNIQUE/FK constraints,
concurrent Ensure/AddItem/Reserve, cross-owner/profile/connection, pairing proofs/
expiration/single consume, keyset pagination и transaction composition rollback.
JobRepository: concurrent claim, lease expiry/renew/reclaim, fencing, retry budget,
delayed availability и reconciliation после cancelled import. Migration tests
проверяют upgrade с версии 2, сохранение User, полный down до 0 и повторный up.
Все интеграционные проверки используют изолированные БД PostgreSQL.

Telegram/auth: все команды, inline callbacks, private sender ownership,
ошибки без secrets, ограничение titles/страниц, HTTP schema/context, Bot API wire
protocol, polling dedup/failures/panic. PostgreSQL: code TTL 5 минут, hash-only storage,
12 конкурентных redeem, expiry, rollback при ошибке session INSERT, replace code,
revoke pending codes, concurrent revoke/redeem и изоляция пользователей.

Extension API: auth/revoke, exact JSON schemas, credential rejection, request size,
CORS/preflight, per-IP/owner/claim rate limits, bounded map concurrency/expiry,
OpenAPI refs и внешний spec validator. Real PostgreSQL проверяет 12 параллельных
chunk retries, create replay, key/sequence conflicts, gaps, cross-owner, rollback,
complete/cancel races, HTTP E2E с 200 tracks, SQL completion 50 000 accepted tracks.
Scale fixture использует ANALYZE после bulk seed; это correctness, не benchmark.

Ниже — матрица для дальнейших business use cases. Она не означает,
что все будущие destination integrations уже реализованы или протестированы.
Extension: npm ci && npm run typecheck && npm test, npm run build и build:firefox.
Unit tests покрывают byte/count bounds, ACK loss + worker restart, повторные wakeups,
cancel после uncertain create, logout race, owner/backend isolation, private status
projection, strict metadata, API credential/redirect policy, retry budget и IndexedDB
transactions через fake-indexeddb. Browser installation smoke пока ручной; Firefox
runtime не объявляется проверенным только по успешной сборке. Yandex/Spotify/VK DOM collectors проверяются на HTML fixtures: virtualized node reuse,
scroll overlap, hidden text, missing metadata/ID, unsafe URL, delayed rows,
count/end/loading signals, cancel, SPA navigation/root replacement, bounded batches
и observer cleanup. Геометрия viewport моделируется; live selectors вручную ещё
не проверены. DOM capture подключён к popup/outbox, автоматического UI resume после reload ещё нет;
DemoProducer остаётся отдельным ingestion fixture. Spotify дополнительно проверяет infinite
loading с ростом scrollHeight, disc/header rows, locale links, ARIA/count ambiguity,
backpressure и UTF-8 byte budget на больших metadata.

| Решение | Обязательная проверка перед готовностью реализации |
|---|---|
| DOM-only | Обезличенные fixtures каждого source; network APIs бросают ошибку; hidden JSON/store не читаются; изоляция imports между adapters |
| Virtualized DOM | Повторный render, перестановка/удаление nodes, missing metadata, смена страницы и abort; неизвестный конец всегда partial |
| Outbox | Worker restart, offline, ACK loss, quota full; неизменные sequence/payload, bounded memory |
| Приём | Replay same payload, conflict different payload, out-of-order, дырки, пустой capture, concurrent Append/Seal, duplicate Seal |
| Source identity | Одинаковые title/artist разных записей не сливаются; fallback identity не считается доказательством match между captures; account/profile mismatch показывается пользователю |
| Matching | Cover/live/remaster, разные artists/duration/album, Unicode, пустой поиск и близкие scores; ambiguity не выбирается молча; policy version сохранена |
| Idempotency | Повторный импорт, два source на один destination ID, два worker на один target, существующий remote track; membership остаётся одна |
| Delivery crash | Crash до send, после send до DB commit, delayed response, native key replay; unknown не вызывает слепой retry |
| Target creation | Потеря ответа create, повторный import, rename источника; не создаёт второй target |
| Queue | Реальный PostgreSQL: concurrent SKIP LOCKED, lease expiry/renew, stale generation, max attempts, backoff, rollback |
| Ownership | Cross-user capture/target/item/job/callback, утечка bearer code (threat model ADR-0010), expired/reused pairing, session revoke, Telegram update replay |
| Cancellation | Новые sends останавливаются; завершённые остаются; in-flight unknown reconciliation продолжается |
| Data safety | DTO запрещает unknown fields/oversize/URLs; логи без credentials/raw content; SSRF отсутствует |
| Migration | Чистая БД → schema; upgrade с предыдущей версии; constraints/FK/indexes; проверяемый rollback или forward repair |
| Scale | 50 000 наблюдений, bounded batches; память не растёт пропорционально полной metadata библиотеки; keyset pages и resume без полного re-upload |

Unit tests — normalization/policy/state transitions. Integration tests — pgx и
настоящий PostgreSQL, не только mock repository. Contract tests — каждый destination
против заявленных capabilities, включая preexisting memberships и uncertain effects.
E2E — управляемые DOM fixtures + fake destination, без streaming API и реальных
пользовательских credentials. Реальные страницы только ручная проверка доступного
DOM по разрешению пользователя, без сохранения чувствительных данных.

Нагрузочный baseline должен записывать объём данных, latency, RSS, число SQL
queries, backlog drain rate и ограничение destination. Численные SLO определить
после выбора хостинга/destination; не обещать throughput по одному benchmark.

VK fixtures дополнительно проверяют foreground dialog, query-only navigation,
album list prerequisite, запрет data-audio/data-full-id и 1 200 виртуальных
треков при трёх переиспользуемых DOM nodes. Это correctness, не benchmark.

Selection (ADR-0016): normalized/source key identity, removal/recycling checkbox
на fixtures трёх сервисов, clear/start/cancel, IndexedDB reopen/concurrent counts,
freeze versus change, keyset byte/count budgets, owner/tab/frame/document/source
RPC isolation, lost start ACK, lost HTTP ACK + worker restart, all/selected completeness
и backend cancel. Manual live-browser smoke остаётся непроведённым.

## Import pipeline (ADR-0017)

Unit: exact matcher (versions, duplicate candidates, ambiguity), capped jitter,
worker config limits. PostgreSQL integration: повтор favorites/playlist/album/selection,
source-key cache, повторная проверка provisional mapping, cross-source destination
membership, concurrent aliases, preexisting remote membership, chunk fingerprint
fallback/replay → seal → execution, lost ACK + fresh worker/lease fencing, cancellation
с late effect evidence, owner isolation, retry exhaustion/expired last lease repair,
unsafe destination без sends, bounded concurrency/heartbeat/shutdown.
Contract fake реализует atomic set semantics; это не test реального музыкального бота.
Extension tests проверяют новый enum progress; OpenAPI и bot labels обновлены вместе.
