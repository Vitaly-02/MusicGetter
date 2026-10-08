# PostgreSQL: domain model и persistence

Реализованы миграции 00001–00007, expected schema version — 7. Schema `musicgetter`,
UUID через `gen_random_uuid()`, timestamptz, bigint для Telegram IDs, duration и
позиций. Нет pgcrypto/ORM или стороннего генератора UUID. PostgreSQL 17 в Compose.

## Сущности и constraints

| Domain entity / таблица | Назначение | Идемпотентность / связи |
|---|---|---|
| User / users | Владелец, Telegram user ID | UNIQUE telegram_user_id, ID > 0 |
| ExtensionPairing / extension_pairings | Code/challenge SHA-256 hashes, owner, expires/consumed/revoked timestamps | UNIQUE code_hash; hash 32 bytes; challenge nullable только при известном owner; одноразовое consume |
| ExtensionSession / extension_sessions | Собственная extension session, owner, expires/revoked | UNIQUE token_hash (SHA-256, 32 bytes), plaintext не хранится |
| telegram_updates | Claim управляющего бота без payload | PRIMARY KEY(bot_id,update_id) |
| SourceProfile / source_profiles | Отдельный аккаунт источника, пользовательский profile key и label | UNIQUE (owner_id,source,profile_key); не хранит credentials стриминга |
| SourceCollection / source_collections | favorites/playlist/album/selection, title, collection key, provisional flag | UNIQUE (profile_id,collection_key); FK на owner/source/profile |
| DestinationConnection / destination_connections | Adapter/account namespace без credentials | UNIQUE (owner_id,adapter,account_key) |
| DestinationCollection / destination_collections | favorites/playlist/album | UNIQUE (connection_id,external_key); один favorites на connection |
| CanonicalTrack / canonical_tracks | Исходные metadata, normalized fields, fingerprint v1, optional source key/URL | GENERATED identity_key; UNIQUE (profile_id,identity_key) |
| DestinationTrack / destination_tracks | Local UUID, внешний track key, metadata | UNIQUE (connection_id,external_key) |
| TrackMapping / track_mappings | Принятое соответствие, origin и policy version | UNIQUE (canonical_track_id,connection_id); owner/connection FK к destination track |
| DestinationMembership / destination_memberships | Единственная запись намерения/подтверждения добавления | UNIQUE (collection_id,destination_track_id); постоянный UNIQUE operation_key |
| Import / imports | Owner, source/destination collections, request key, state | UNIQUE (owner_id,request_key); обе коллекции принадлежат одному owner |
| import_uploads | Capture state и counters для created/receiving Import | PK import_id, owner FK, SHA-256 create/complete digest |
| import_chunks | Durable ACK без raw JSON payload | PK(import_id,sequence), UNIQUE(import_id,idempotency_key), digest/received/added |
| ImportItem / import_items | Canonical track, первая позиция, state, optional selected destination track | UNIQUE (import_id,canonical_track_id); profile/source/connection совпадают с import |
| ImportJob / import_jobs | item, kind, logical key, attempts, schedule, lease/worker/generation | UNIQUE (item_id,kind) и (import_id,kind,logical_key); item принадлежит этому import |

SourceProfile и DestinationConnection — вспомогательные сущности для изоляции
аккаунтов; кроме них представлены все 11 запрошенных domain entities. Все owned
связи используют составные FK, а не только проверку в handler. UUID глобально
уникален, но сам по себе не авторизация. Cross-owner/source-profile/destination-
connection ссылки отвергаются даже при прямом SQL INSERT.

SQL domains ограничивают source (spotify/yandex/vk) и два разных типа collection
kind. `selection` допустим только у source. Metadata constraints запрещают пустой
title/artist, NULL elements/пустой массив artists, отрицательную duration/position.
Duration ограничена безопасным JS integer range; NULL не равен известному нулю.
Source URL в БД ограничен approved source origins без query/fragment; Go также
отбрасывает query/fragment и запрещает userinfo. URL никогда не загружается сервером.

## Canonical identity и fingerprint

`CanonicalTrack` — нормализованное наблюдение источника, не доказательство
тождественности акустической записи. PrepareCanonicalTrack вычисляет поля заново;
repository не принимает доверенный fingerprint от клиента.

Алгоритм v1: Unicode NFKC → Unicode case folding → NFKC → trim/collapse whitespace.
Artists нормализуются по отдельности, сортируются и дедуплицируются. Пунктуация,
диакритика, live/remaster qualifiers сохраняются. Альбом и edition нормализуются
тем же способом. Duration используется точно в ms, без округления; unknown — null.

SHA-256 считается от детерминированного JSON с фиксированным порядком полей:
`version,title,artists,album,duration_ms,edition`. Формат fingerprint — `v1:` +
64 lowercase hex characters. Arrays/JSON исключают неоднозначность разделителей.
URL, source key, owner и source не входят в digest; профиль/источник задают DB scope.
Unicode tables/dependency version зафиксированы go.mod, формат закреплён golden test;
изменение семантики требует новой версии, а не тихого пересчёта существующих строк.

БД сама генерирует identity_key:
- Есть непустой source_track_key: `key:<source_track_key>`.
- Нет source_track_key: `fp:<fingerprint>`.

Профиль делает fallback локальным для пользователя/аккаунта. Два известных разных
source keys с одинаковыми metadata остаются отдельными canonical tracks. При
отсутствии ID точные одинаковые metadata дедуплицируются; ошибку одинакового
описания двух разных записей невозможно исключить без дополнительных данных.
Изменение metadata без стабильного key создаёт новую identity. Переключение между
наблюдениями с key и без key не создаёт автоматической alias-связи. Такие совпадения
требуют matching/review; конечная membership всё равно уникальна по destination ID.

Canonical metadata immutable: повтор source key возвращает первоначальную запись,
не меняет evidence существующих imports. Полноценная история metadata revisions и
capture snapshots — следующий этап. По ADR-0018 успешный mapping, включая provisional track, переиспользуется без
повторного remote search. Он сохраняет принятое решение, не доказывает audio identity;
исправление ошибочного mapping требует явной инвалидации.

## Repository layer

Небольшие concrete repositories в internal/storage/postgres: User, Pairing,
Source, DestinationCollection, CanonicalTrack, DestinationTrack, Mapping, Membership,
Import и Job. `DBTX` — только Exec/Query/QueryRow, принимается pgxpool или pgx.Tx.
Application может объединить track + item + job в одной транзакции; repositories
не коммитят чужую транзакцию. SQL остаётся явным, sqlc пока не добавлен.

Ensure/Create/Reserve используют ON CONFLICT с возвращением существующей записи.
No-op UPDATE выбран для корректного RETURNING при конкурентном INSERT; DO NOTHING
+ SELECT в одном snapshot мог бы не увидеть конкурирующую запись. Цена — row locks
и дополнительные tuple versions при replay; нужны bounded batches и обычный VACUUM.
Сохраняются ID, исходная позиция item, job state/attempts и membership operation key.
Переименование collection обновляет title; смена kind по прежнему ключу запрещена.
Повтор request key с другими collections и изменение принятого mapping дают conflict.

Чтение owned entities фильтруется по owner. Import/Item lists используют keyset по
UUID и limit 1..200; порядок traversal — UUID, не хронология и не playlist position.
Для UI с хронологией/консистентным concurrent snapshot нужен отдельный cursor contract.
SQL errors переводятся в safe domain errors (Invalid/NotFound/Conflict/Storage),
context cancellation сохраняется; raw DSN, SQL detail и пользовательские metadata
наружу не выдаются. Incoming UUIDs должны валидироваться transport layer.

## Membership и jobs

Membership `reserved` — durable intent, `unknown` — неопределённый внешний эффект,
`applied` — подтверждённое наличие. Reserve не сбрасывает state. CAS transition
разрешает reserved → unknown/applied и unknown → applied. Operation key неизменен
между импортами. UNIQUE физически не допускает двух строк для одной пары target/
track; это не exactly-once удалённой отправки. До реализации adapter/reconciliation
никаких внешних эффектов эти методы не делают.

JobRepository реализует importer.JobQueue. Claim — UPDATE из CTE с FOR UPDATE
SKIP LOCKED, фиксирует lease/worker/generation/attempts. Использовать pool для
короткой autocommit операции; если передан Tx, caller обязан commit до внешнего I/O.
Renew/Complete/Retry/Fail проверяют owner, job, worker, generation и неистёкший lease.
Expired recovery ограничен limit, увеличивает generation; attempts >= max дают
failed. Retry откладывает available_at. Enqueue никогда не оживляет completed/failed
job. Claim не начинает match/deliver для terminal import; reconcile допускается,
как низкоуровневая возможность очереди; автоматический cancelled reconciliation
пока не подключён. Worker обязан повторно проверять
отмену перед внешним эффектом. Worker, backoff и delivery pipeline реализованы в ADR-0017; реальные destination adapters отсутствуют.

## Миграции и дальнейшие этапы

00002 создаёт аккаунты/коллекции; 00003 — tracks/mappings/memberships;
00004 — imports/items/jobs. Новые таблицы не переписывают старую business data;
DDL и UNIQUE indexes выполняются на новых таблицах внутри migration transactions.
Goose lock сериализует runners. Readiness version 7 несовместим со старым бинарником
version 6: сначала coordinated upgrade, для rolling deploy нужен expand/contract.

Down удаляет соответствующие таблицы вместе с данными. В dev/test можно проверить
на изолированной БД; для рабочей базы — backup и остановка writers, предпочтительно
forward repair. Нет CASCADE на неизвестные объекты. 00001 по-прежнему удаляет
только пустую schema через RESTRICT. Короткий retention ledger запрещён: удаление
membership/history idempotency keys разрушает гарантию повторного импорта.

00005 добавляет sessions, update dedup и bot-issued pairing; миграционные
блокировки/rollback описаны в ADR-0010. SessionRepository владеет короткими
транзакциями для issue/redeem/revoke, блокирует user row перед writes. BotQueries
читает owner-scoped imports по (created_at DESC,id DESC), limit 10,
агрегирует статус одного import в SQL; playlists имеют UUID keyset.

00006 реализует import_uploads/import_chunks. API теперь создаёт created import (с 00007), затем receiving,
пишет canonical tracks/items постепенно, а complete создаёт jobs одной транзакцией.
Детали uniqueness, counters, лимитов и rollback — ADR-0011. UploadRepository сам
владеет транзакциями; внешние destination calls отсутствуют.

Ещё не реализованы candidate history, collection mappings, target creation operations
(worker execution реализован). HTTP endpoints приёма и progress уже защищены extension Bearer.

## Pipeline migration 00007

Vocabulary Import/ImportItem, error_code и индекс failed jobs добавлены ADR-0017.
Migration требует остановки процессов; UPDATE/ALTER/CREATE INDEX держат table locks.
Down сохраняет membership ledger/keys/unknown, преобразует vocabulary обратно и
теряет error_code. Подробности совместимости и ограничения — ADR-0017.
PipelineRepository блокирует parent import → leased job, проверяет generation и
DB clock до commit. Match checkpoint, membership intent, terminal item/job/import
согласованы транзакциями; внешний вызов всегда вне transaction.
