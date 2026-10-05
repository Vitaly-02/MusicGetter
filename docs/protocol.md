# Проект протокола extension ↔ backend ↔ управляющий bot

JSON поверх HTTPS, prefix /v1, серверная валидация схем и лимитов обязательна.
Pairing/session handlers реализованы; маршруты импорта ниже остаются проектом.
Никаких multipart pages, произвольных metadata blobs или streaming credentials.

## Привязка

Реализованный flow (ADR-0010):

1. Пользователь в личном чате вызывает `/connect`. Бот выдаёт 26-символьный
   Base32 code из 128 random bits, TTL ровно 5 минут. Новый код заменяет старый.
2. Extension отправляет `POST /v1/pairings/redeem`, Content-Type application/json,
   body `{"code":"..."}` (до 1024 bytes, unknown fields отклоняются). Owner не передаётся.
3. В одной транзакции code потребляется и создаётся 30-дневная session.
   Ответ 201: `{"token":"mge_...","tokenType":"Bearer","session":{"id":"...","ownerId":"...","expiresAt":"..."}}`.
   Cache-Control: no-store. Повтор/expired/revoked code: 401; DB failure: 503.
   Если ответ потерян, требуется новый `/connect`; восстановить plaintext token нельзя.
4. `GET /v1/extension/session` с `Authorization: Bearer mge_...` возвращает
   session (200) либо 401 при invalid/expired/revoked token. Чтение БД на каждом запросе.
5. `/settings` → «Отозвать все подключения» отзывает все sessions и pending codes.

Код — bearer secret нашего сервиса: не публиковать и не пересылать. Backend хранит
только SHA-256 code/token. Extension storage с доступом trusted contexts ещё предстоит
реализовать вместе с расширением. Никакие streaming cookies/tokens не участвуют.
Production transport — HTTPS; HTTP только для локальной разработки на loopback.

Старый extension-first challenge/verifier flow остаётся только repository contract,
публичного POST /v1/pairings нет. Bot-issued redeem принимает только коды нового flow.
Telegram Bot API используется через long polling; webhook пока не реализован.
Повтор update_id не выполняет handler повторно. Claim перед handler допускает потерю
ответа при crash: новое сообщение с той же командой — новая попытка (см. ADR-0010).

## Приём

| Маршрут | Назначение |
|---|---|
| POST /v1/captures | source profile, collection key/kind/title, target; Idempotency-Key |
| PUT /v1/captures/{id}/batches/{sequence} | BatchEnvelope v1, server digest, atomic receipt |
| GET /v1/captures/{id} | owner-only state, contiguousThrough и ограниченная page batch receipts |
| POST /v1/captures/{id}/seal | lastSequence, summary; immutable replay |
| POST /v1/captures/{id}/abort | отменить ещё не sealed сбор |
| GET /v1/imports | keyset pagination |
| GET /v1/imports/{id} | прогресс, состояние, summary ошибок |
| POST /v1/imports/{id}/cancel | остановить новые эффекты |

BatchEnvelope определён в extension/src/core/contracts.ts. Path и body
captureId/sequence обязаны совпадать. Owner/source/target нельзя переопределить
batch-ом. Server digest — SHA-256 от канонических валидированных значений DTO,
а не от произвольного порядка JSON keys; algorithm/version фиксируются schemaVersion.
Go AppendBatch — внутренний use case input: digest вычислен trusted API layer.

Лимиты: 200 items, 512 KiB body, 4 outstanding batches; server проверяет оба
ограничения. Строки ограничены по длине (title/album 1024, artist 512, key 512
символов; максимум 32 artists); durations/positions неотрицательны и в безопасном
для JavaScript целочисленном диапазоне. Поля title и непустой artists обязательны;
недостаточные карточки дают warning, не выдуманные metadata. Unknown properties
отклонять. Error response: code, requestId, retryAfter при необходимости;
не возвращать raw SQL, HTML или credential-bearing messages.

Sequence с нуля, replay отправляет неизменённое содержимое. Backend может принять
out-of-order batches; seal принимает только непрерывный диапазон. Capture из 0
items использует lastSequence=-1. Потерянный ACK восстанавливается replay/receipts.
Ошибки: 401/403 — остановить и перепривязать/показать отказ; 409 — payload/state
conflict, не retry; 413/422 — исправить до приёма; 429/503 — backoff с jitter и
Retry-After. Уже принятое содержимое sequence нельзя заменить после уменьшения
batch. На исчерпании квоты pause DOM collection, не терять observations молча.

CORS ограничить согласованными extension origins, но CORS не является auth.
Token scopes, ownership, request size/rate limits и проверка sender обязательны.
Любые labels/source keys — недоверенный пользовательский ввод; экранировать в
Telegram/HTML, не выполнять их и не загружать server-side URLs из DTO.

## Управляющий Telegram bot

Application use cases общие с API: привязать/отозвать session, выбрать connection
и target, список импортов, прогресс, cancel/retry, review candidates, настройки и
список/mapping коллекций. Для текущих read-only callbacks data содержит UUID import/cursor, owner проверяется
по sender; revoke — явная фиксированная команда в settings. Для будущих review
операций callback data содержит opaque ID действия, а server
проверяет owner, текущую версию item и срок действия; не доверять ID из callback.
Управление удалёнными playlist (создание/переименование/удаление) зависит от
capabilities; destructive операции требуют явного отдельного действия пользователя.
В MVP только выбор существующего target и идемпотентное создание при поддержке;
remote rename/delete/reorder отложены, их интерфейсы не объявляются фиктивно.
