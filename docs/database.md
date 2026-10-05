# PostgreSQL: логическая схема

Ниже проект domain-схемы, пока не реализованный в DDL. Bootstrap migration
создаёт только namespace `musicgetter`; Goose ведёт `public.goose_db_version`. Все ID — UUID, времена — timestamptz
в UTC, source/kind/status — text с CHECK. Счётчики и позиции — bigint. У каждого
владельческого ресурса owner_id; связи между ними должны включать owner_id в
составных FK или обеспечиваться эквивалентной транзакционной проверкой. Одной
проверки UUID в HTTP handler недостаточно.

| Сущность | Назначение и ключевые поля | Ограничения / индексы |
|---|---|---|
| users | id, telegram_user_id bigint, settings, created_at | UNIQUE telegram_user_id |
| pairing_requests | id, code_hash, challenge_hash, expires_at, confirmed_user_id, consumed_at | UNIQUE code_hash; атомарное одноразовое погашение |
| extension_sessions | id, owner_id, token_hash, scopes, expires_at, revoked_at | UNIQUE token_hash; index owner_id |
| source_profiles | id, owner_id, source, label | UNIQUE (owner_id,id,source); не хранит streaming account credentials |
| source_collections | id, owner_id, profile_id, source_key, provisional, kind, title | UNIQUE (profile_id,source_key); для provisional key включать capture namespace |
| source_tracks | id, owner_id, profile_id, source_key, provisional, latest_metadata | UNIQUE (profile_id,source_key); provisional — capture-scoped |
| captures | id, owner_id, collection_id, target_id, state, schema_version, last_sequence, summary, created_at | immutable после seal; index (owner_id,created_at,id) |
| ingestion_batches | capture_id, sequence, digest, item_count, accepted_at | PK (capture_id,sequence); sequence >= 0 |
| capture_items | id, capture_id, source_track_id, observation_key, position, metadata_snapshot | UNIQUE (capture_id,observation_key); index (capture_id,position,id) |
| destination_connections | id, owner_id, adapter_kind, external_account_ref, secret_ref, capabilities_snapshot | секреты отдельно; index owner_id |
| destination_targets | id, owner_id, connection_id, external_target_id, kind, title | UNIQUE (connection_id,external_target_id); kind проверяется adapter |
| collection_mappings | owner_id, source_collection_id, connection_id, target_id | UNIQUE (source_collection_id,connection_id); переименование не создаёт target |
| target_operations | id, connection_id, source_collection_id, operation_key, status, receipt | UNIQUE operation_key и (connection_id,source_collection_id); ledger создания target |
| imports | id, owner_id, capture_id, target_id, state, policy_version, cancel_requested_at | UNIQUE capture_id; index (owner_id,created_at,id) |
| import_items | id, owner_id, import_id, capture_item_id, state, selected_track_id, error_code | UNIQUE (import_id,capture_item_id); index (import_id,state,id) |
| match_candidates | item_id, rank, destination_track_id, metadata, score, reasons, policy_version | PK (item_id,rank); ограниченный top N |
| match_decisions | id, item_id, destination_track_id, origin, policy_version, decided_at | origin automatic/user; история выбора, current choice в import_items |
| destination_memberships | id, owner_id, connection_id, target_id, destination_track_id, state, verified_at | UNIQUE (connection_id,target_id,destination_track_id); FK target к той же connection |
| delivery_operations | id, membership_id, operation_key, state, receipt, attempt, error_code | UNIQUE membership_id; UNIQUE operation_key; index (state,updated_at,id) |
| item_deliveries | item_id, operation_id | PK item_id; много items разделяют одну membership/operation |
| jobs | id, owner_id, import_id, kind, logical_key, payload, status, available_at, lease_until, generation, worker_id, attempts, last_error_code | UNIQUE logical_key; частичный index (available_at,id) WHERE status='ready'; index lease_until WHERE status='leased' |
| request_keys | owner_id, operation, key, digest, resource_id, expires_at | PK (owner_id,operation,key); replay different digest — conflict |
| telegram_updates | bot_key, update_id, processed_at | PK (bot_key,update_id); idempotency входных команд |

Metadata snapshot capture неизменяем; обновление latest_metadata не изменяет
прошлый import/matching evidence. Album в metadata — название, а source collection
kind=album — самостоятельная коллекция; не объединять одноимённые альбомы.

В MVP captures импортируются добавлением. Source deletion не каскадирует в remote
playlist. Destination target нельзя пересоздавать по названию при повторном импорте;
использовать mapping и ledger. При явном remap создаётся новый import/capture.

## Транзакционные границы

- Append: проверить owner/state с блокировкой capture; вычислить canonical digest;
  вставить batch и observations вместе. Повтор того же digest возвращает исходный
  receipt. Другой digest — conflict. Seal конкурирует с Append через ту же блокировку.
- Seal: проверить весь диапазон 0..last_sequence без дырок и отсутствие лишних
  batches; создать один import + items + начальные jobs; зафиксировать seal.
  Replay тех же параметров возвращает import, изменение summary/sequence — conflict.
- Claim: SELECT ready jobs по available_at,id FOR UPDATE SKIP LOCKED LIMIT N;
  UPDATE status/lease/generation и commit. Никаких HTTP calls внутри транзакции.
- Lease: Renew/Complete/Retry/Fail используют job_id, worker/generation и проверку
  неистёкшего lease. Reaper возвращает expired leased jobs в ready с новым fencing
  generation. Reclaim ensure job сначала проверяет delivery ledger и reconciles.
- Ensure: INSERT membership ON CONFLICT; все конкурирующие items присоединяются
  к одному delivery operation. Commit intent ДО запроса destination. После ответа
  атомарно сохранить outcome + item state + необходимые дальнейшие jobs.
- Outbox для Telegram notifications при необходимости — разновидность jobs,
  создаваемая в той же транзакции. Не обещать отсутствие повторных уведомлений,
  если Telegram отправка не даёт доказуемого idempotency key.

Progress строится по текущим import_items, не числу attempts. При появлении дорогих
COUNT добавлять агрегаты только транзакционно вместе со state transition, с
возможностью пересчёта. Фильтрация и keyset pagination по owner_id обязательны.

## Retention и повторные импорты

Неподтверждённые captures можно удалять по TTL; ACK'ed capture metadata и истории —
по политике retention. Membership ledger/operation keys нельзя удалять по короткому
TTL вместе с историей import: это сломает idempotency. При удалении пользователем
аккаунта собственные данные удаляются; последующая новая привязка требует чтения
remote membership/atomic ensure, а не предположения о пустом destination.
Не хранить сырые страницы, streaming URL query, streaming secrets или сырой ответ
destination без фильтрации. Для destination credentials — secret_ref, при хранении
секрета в БД только encryption-at-rest с ключом вне БД; hash для собственных bearer.
