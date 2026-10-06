# ADR-0011: Extension HTTP API и транзакционный приём чанков

Статус: принято. Дата: 2026-10-05. Уточняет ADR-0006/0009/0010 и заменяет
проект `/v1/captures` по текущему запросу API `/v1/imports`.

## Модель

Import создаётся сразу в `collecting`; `import_uploads` хранит capture state,
create/complete digests и счётчики. Никаких jobs до успешного `/complete`.
CanonicalTrack и ImportItem сохраняются постепенно в транзакции чанка, без
библиотеки в RAM. После seal SQL INSERT SELECT создаёт match jobs и переводит
import в queued (пустой — completed). Worker/matcher/destination не реализуются.

`client_request_id` уникален внутри owner. SHA-256 от JSON типизированного DTO
с фиксированными Go field order фиксирует исходный запрос. UUID destination
обязательно принадлежит owner из проверенного Bearer token. Source profile и
collection создаются внутри той же транзакции; profile label не перезаписывается.

Чанк: schema_version=1, sequence от 0, idempotency_key, 1–200 tracks, максимум
512 KiB. Import ограничен 100 000 observations и 10 000 chunks. До четырёх запросов
in flight рекомендованы расширению. POST /tracks атомарно записывает metadata,
items, receipt и counters, ACK только после commit. UNIQUE(import_id,sequence),
UNIQUE(import_id,idempotency_key) и UNIQUE(import_id,canonical_track_id) закрывают
разные уровни дедупликации. Digest вычисляется сервером; JSON whitespace/порядок
полей не влияют, порядок массива и metadata влияют. Optional absent/null pointer
поля и absent/empty optional strings канонизируются типизированным DTO.

Replay того же key/sequence/digest возвращает исходные received/added и replay=true,
даже после seal/cancel: ACK подтверждает уже принятые данные и не оживляет import.
Изменение digest, sequence или key при занятом receipt даёт 409. При повторном
наблюдении canonical track сохраняется первая принятая позиция, не минимальная
при out-of-order chunks. Set semantics сохраняется, точный playlist order не обещан.

Append/complete/cancel блокируют import + upload row в одном порядке. Out-of-order
chunks разрешены; contiguous_through хранит последний непрерывный sequence.
Complete требует exact range 0..last_sequence, отсутствие лишних chunks и
observed_count == сумме observations до track dedup. Полнота complete допустима
только с visible_end_confirmed; остальные причины — partial. Это утверждение
клиента о DOM, сервер не доказывает полноту страницы. Неизменённый complete replay
безопасен. Cancel предотвращает новые записи и ready jobs, отменяет pending items.
Leased/in-flight external operations обязаны проверять import state/reconcile;
отмена не откатывает уже выполненный внешний эффект.

## HTTP, credentials и лимиты

Все новые endpoints — /v1; [OpenAPI 3.1.1](../openapi.json) фиксирует snake_case DTO.
Старые /v1/pairings/redeem и /v1/extension/session оставлены deprecated aliases
с прежними response shapes, под теми же middleware и общими лимитами.
Общие ошибки остаются совместимыми: error.code/message, requestId.

Strict JSON: exact case-sensitive allowlist полей, нет unknown/duplicate keys,
null запрещён для непустых обязательных полей, UTF-8, bounded body. Не принимаются
Cookie, credential-bearing custom headers, streaming Bearer, unknown query params,
source URL query/fragment/userinfo, HTML fields или произвольные blobs. Metadata
строки являются пользовательским текстом, не credentials API. Source URL только
проверяется локально по source allowlist; backend не выполняет source requests.

CORS — точные EXTENSION_ORIGINS: chrome-extension://ID либо moz-extension://UUID;
wildcard, https origins, null и cookies запрещены. Пустой список закрывает все
Origin-bearing requests. Запрос без Origin допустим для extension service worker/
локального клиента, но всегда требует Bearer (кроме claim). CORS не авторизация.

Rate limiting — bounded in-memory fixed-minute windows на процесс: 120/IP,
60/owner, 5/IP для claim. Owner quota общая для всех его sessions; aliases делят
claim quota. Map до 10 000 keys, затем fail closed, expired keys удаляются лениво.
429 содержит Retry-After. Перезапуск сбрасывает лимиты, replicas не разделяют budget.
Это выбранный локальный MVP; для нескольких replicas нужны общий edge limiter или
PostgreSQL limiter. X-Forwarded-For не доверяем: за proxy без отдельного доверенного
слоя клиенты делят IP quota. Production TLS и ограничения соединений задаёт edge.

## Миграция

00006 расширяет state CHECK и создаёт upload/receipt tables. ALTER imports берёт
ACCESS EXCLUSIVE и проверяет существующие rows; coordinated deploy, перед большой
рабочей БД оценить lock duration. Down удаляет digests/receipts, переводит collecting
imports в cancelled, не удаляет tracks. После down нельзя возобновлять прежние
uploads: потеря receipt history разрушает replay guarantee. На production — backup,
остановка writers и предпочтительно forward repair. Короткий retention receipts
не вводится: ACK должен восстанавливаться при позднем retry.

## Проверки

Unit: strict DTO, limits, CORS, credentials, owner propagation, rate concurrency/
expiry, digest determinism. Real PostgreSQL: create/chunk races, direct UNIQUE,
rollback половины чанка, gaps, cross-owner, late replay, seal/cancel races, jobs
после seal. HTTP E2E с 200 tracks. Завершение 50 000 предварительно принятых tracks
проверяется set-based SQL; это проверка correctness, не throughput/RSS benchmark.
OpenAPI проверяется внешним validator и локальными tests references/routes.
