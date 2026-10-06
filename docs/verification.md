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
что pipeline/source/destination уже реализованы или протестированы.
TypeScript: `tsc -p extension/tsconfig.json` после установки toolchain; зафиксировать
версии и lockfile с первым package.json. MV3 runtime/build пока не существует.

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
