# ADR-0009: Canonical identity, tenant constraints и persistence

Статус: принято. Дата: 2026-10-05. Уточняет ADR-0004, 0006 и 0007 по текущему
запросу domain model и PostgreSQL schema.

## Решение

Вместо проектных source_tracks/destination_targets используются canonical_tracks
и destination_collections. Все 11 запрошенных entities реализованы, плюс SourceProfile
и DestinationConnection для account scope. Source collection kinds отделены от
destination: selection только у источника. Capture ingestion остаётся отдельным этапом.

Стабильный rendered key предпочтителен, но не обязателен. При его отсутствии
используется versioned SHA-256 fingerprint от нормализованных title/artists/album/
duration/edition. Unicode NFKC/casefold, artist set sorting, фиксированный JSON.
Длительность точная, missing отличается от zero; live/remaster, punctuation и
accent не вырезаются. Fingerprint не использует URL или streaming credentials.

Ранее provisional identity планировалась только внутри capture. Теперь canonical
metadata identity может переиспользоваться в рамках profile между импортами, чтобы
дать deterministic fallback. Это не акустический ID и не автоматическое согласие
на matching: при одинаковом описании разных записей нужна review. Stable keys
не объединяются только по fingerprint, key/no-key aliases автоматически не создаются.
Metadata canonical record immutable, corrections/revisions — отдельная будущая задача.

SQL GENERATED identity_key + UNIQUE(profile_id,identity_key) закрывают fallback
дедуп. UNIQUE(import_id,canonical_track_id) закрывает повторную отправку track.
UNIQUE(collection_id,destination_track_id) и стабильный operation key закрывают
повтор reservation между импортами. Составные owner/profile/connection FK делают
некорректные межпользовательские ссылки невозможными на уровне БД.

ON CONFLICT no-op UPDATE + RETURNING возвращает сохранённый ID и outcome даже при
конкурентном insert. Не использовать DO NOTHING/SELECT в том же snapshot без
учёта конкурирующей транзакции. Read limits 200, keyset по UUID. pgx repositories
можно связать одним Tx; no-op updates требуют VACUUM при большом количестве replay.

Durable jobs реализованы как repository, не как import worker. Claim SKIP LOCKED,
leases, fencing, retry budget, bounded reclaim. Membership reserved/unknown/applied
не заменяет подтверждение внешнего сервиса. Автоматических sends пока нет.

## Альтернативы

Дедуп только по source key отвергнут: DOM может не давать его. Глобальный UNIQUE
fingerprint отвергнут: одинаковые metadata не доказывают одну запись. Fuzzy matching
в fingerprint отвергнут: он дал бы нестабильное схлопывание covers/remasters.
Универсальный CRUD Store/ORM и sqlc пока не нужны для небольших явных repositories.

## Проверки

Golden fingerprint, Unicode equivalence, delimiter safety и версии записей;
PostgreSQL direct duplicate INSERT, concurrent Ensure/Reserve/AddItem, cross-tenant
FK, one-time pairing, upgrade/down, transaction rollback, pagination, SKIP LOCKED,
lease expiry/stale worker, retry limits, cancellation/reconciliation.

## Источники

- [PostgreSQL constraints](https://www.postgresql.org/docs/17/ddl-constraints.html)
- [Unicode case folding в Go](https://pkg.go.dev/golang.org/x/text/cases)
- [Unicode normalization в Go](https://pkg.go.dev/golang.org/x/text/unicode/norm)
