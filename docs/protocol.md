# Проект протокола extension ↔ backend ↔ управляющий bot

JSON поверх HTTPS, prefix /v1, серверная валидация схем и лимитов обязательна.
Это проект маршрутов; OpenAPI и handlers появятся вместе с реализацией.
Никаких multipart pages, произвольных metadata blobs или streaming credentials.

## Привязка

1. Extension создаёт локальный высокоэнтропийный verifier и отправляет его hash
   challenge в POST /v1/pairings. Backend возвращает одноразовый code и Telegram
   deep link; TTL 5 минут, лимиты по устройству/IP, код хранится только hash.
2. Пользователь открывает управляющего бота, видит запрос привязки и явно
   подтверждает его. Telegram user ID берётся из проверенного Telegram update.
3. Extension погашает code + verifier через POST /v1/pairings/redeem. Одноразовый
   atomic consume выдаёт scoped MusicGetter session. Сам deep link не даёт session;
   code без verifier не может привязать чужой экземпляр extension.
4. Session хранится только в extension storage с доступом trusted contexts;
   используется в Authorization к backend; отзыв через bot/settings. Истечение
   требует повторной привязки в первой версии, refresh rotation отложен.

Это credential нашего сервиса. Cookies/tokens стриминга не читаются ни на одном
шаге. Telegram Bot API допустим для нашего управляющего бота; запрещены source API.
В production webhook проверяет Telegram secret; local dev может использовать
long polling, но одновременно один способ доставки. Повторные update_id не
дублируют команды. Точные integration details проверяются при реализации.

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
список/mapping коллекций. Callback data содержит opaque ID действия, а server
проверяет owner, текущую версию item и срок действия; не доверять ID из callback.
Управление удалёнными playlist (создание/переименование/удаление) зависит от
capabilities; destructive операции требуют явного отдельного действия пользователя.
В MVP только выбор существующего target и идемпотентное создание при поддержке;
remote rename/delete/reorder отложены, их интерфейсы не объявляются фиктивно.
