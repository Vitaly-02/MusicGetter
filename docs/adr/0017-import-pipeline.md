# ADR-0017: Durable import pipeline и безопасная доставка

Статус: принято. Дата: 2026-10-07. Уточняет ADR-0003/0004/0009/0011.

## Исполнение и состояния

Приём уже выполняет validation → normalization v1 → fingerprint → input dedup
в транзакции каждого chunk. Worker не пересчитывает identity новым алгоритмом.
Создание HTTP import даёт created, первый новый chunk — receiving, seal — queued
(пустой capture сразу completed). Worker переводит queued → processing.
Terminal states: completed, completed_with_errors, cancelled, failed. Item states:
pending, searching, matched, ambiguous, not_found, already_present, added, failed.
Отмена непройденного item — failed/error_code=import_cancelled. Внешний эффект,
подтверждённый после отмены, сохраняется; import остаётся cancelled.

Один job kind=match проходит весь item; checkpoint matched сохраняет выбор,
поэтому повтор доставки не начинает поиск заново. Job kind сохранён для
совместимости storage; отдельные deliver/reconcile jobs сейчас не создаются.
Все переходы проверяют owner, lease generation/worker/deadline; parent import
блокируется перед job, как при cancellation. Последний terminal item атомарно
закрывает import. ambiguous/not_found/failed дают completed_with_errors; failed
на уровне import зарезервирован для административного abort всего импорта.
Review UI, ручной повтор terminal items и история candidates — отдельные задачи.

## Matching и кэш

Matcher exact_metadata_v1 сравнивает fingerprint нормализованных title, artist set,
album, duration и edition, не доверяя adapter score. Разные candidate IDs с теми
же metadata — ambiguous; отсутствие exact candidate — not_found. Одна страница
максимум 50 candidates; непустой cursor или превышение лимита — ambiguous:
невидимая часть результата не доказывает уникальность. Это консервативная политика,
а не акустическое распознавание или гарантия корректного совпадения.

Manual mapping переиспользуется. Automatic mapping текущей policy можно использовать
без поиска только при стабильном source key; metadata-only observations проходят
повторный поиск. Изменённый mapping никогда не перезаписывает прежний молча.
Known source IDs с одинаковыми metadata не схлопываются по ADR-0009. Без ID действует
fingerprint fallback. Разные source identities, совпавшие с одним destination ID,
дедуплицируются общей membership независимо от source kind и source service.

## Внешний эффект

Автоматическая отправка разрешена **только** при AtomicEnsureMembership и поддержке
kind. Это более строго, чем общий capability interface ADR-0005. Контракт требует
атомарной set semantics (account,target,track), включая preexisting membership,
конкурентных внешних writers и повтор после произвольного timeout. Native request
idempotency сама по себе недостаточна. Небезопасный/неподключённый destination
завершает item ошибкой без отправки. Adapter должен подтвердить capability реальными
contract tests; флаг без доказанного протокола не является реализацией гарантии.

INSERT ON CONFLICT DO NOTHING резервирует membership; отдельный SELECT после
conflict wait получает свежий READ COMMITTED snapshot. Stable operation key общий
для всех imports. До outbound I/O ledger становится unknown. applied ledger
сразу даёт already_present. Optional Contains может подтвердить наличие, но false
не доказывает отсутствие in-flight effect. Для прежнего unknown сначала вызывается
optional Reconcile по operation key (adapter должен уметь lookup без receipt).
Pending ждёт; положительный результат фиксируется; unknown допускает повтор
только через гарантированный atomic ensure. Applied/already_present подтверждают
ledger и item в одной транзакции с завершением job.

После исчерпания попыток item failed, unknown ledger сохраняется. Он не превращается
в «точно не добавлено». Следующий import использует тот же intent и безопасный ensure.
После cancellation новые sends запрещены; in-flight запрос нельзя отозвать гарантированно.
Если подтверждение потеряно вместе с процессом, cancelled import не запускает новые
writes. Unknown остаётся для будущего import/ручной reconciliation; автономного
reconciliation daemon для отменённых/terminal imports сейчас нет. Exactly-once
сетевых вызовов не обещается; отсутствие повторного membership effect зависит от
проверенного atomic destination contract. Внешние удаления из target не отслеживаются:
сохранённый applied ledger трактуется как уже импортированный track.

## Worker и границы

SKIP LOCKED claim по одному item на свободный slot, 1–64 slots/process (default 4).
Lease default 30s, heartbeat lease/3, job timeout 2m; contexts передаются всем портам.
SIGTERM прекращает claim и отменяет I/O, незавершённые leases заберёт следующий worker.
Адаптер обязан соблюдать context. Retry до 8 попыток (DB budget), exponential backoff
с equal jitter, default 1s–5m. Reaper возвращает expired lease либо исчерпывает budget;
bounded repair закрывает оставшиеся items и import после последнего expired lease.
SQL transactions короткие, никогда не охватывают внешний I/O. DB/adapter errors
логируются фиксированными event/code без metadata, credentials и raw transport errors.
Лимит конкурентности per process; fleet budget определяется числом replicas.

cmd/worker и Compose profile worker готовы, но registry пуст: конкретный музыкальный
destination не выбран. Production не содержит fake adapter. Integration tests
используют atomic contract fake и реальный PostgreSQL. Это не проверка реального бота.

## Миграция 00007

Требуется остановить API/bot/worker и обновить extension (изменились enum значения).
Миграция transactional: CHECK constraints и state updates берут table locks и могут
переписать много строк; index строится обычным CREATE INDEX. Планировать maintenance
window и время блокировки по размеру таблиц. Backfill collecting→receiving,
running/needs_attention→processing; needs_review→ambiguous, ensuring/reconciling→matched,
skipped→not_found, cancelled item→failed с error_code. Receipts, jobs, fingerprints,
mappings, membership keys/unknown states сохраняются. Down преобразует vocabulary
обратно, удаляет error_code/index; данные новых состояний сводятся к старым и теряют
часть детализации. До rollback остановить все processes; ledger не удалять.
