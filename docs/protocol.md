# Проект протокола extension ↔ backend ↔ управляющий bot

JSON поверх HTTPS, prefix /v1, серверная валидация схем и лимитов обязательна.
Pairing/session handlers реализованы; chunked import handlers реализованы (ADR-0011).
Никаких multipart pages, произвольных metadata blobs или streaming credentials.

## Привязка

Реализованный flow (ADR-0010):

1. Пользователь в личном чате вызывает `/connect`. Бот выдаёт 26-символьный
   Base32 code из 128 random bits, TTL ровно 5 минут. Новый код заменяет старый.
2. Extension отправляет `POST /v1/pair/claim`, Content-Type application/json,
   body `{"code":"..."}` (до 1024 bytes, unknown fields отклоняются). Owner не передаётся.
3. В одной транзакции code потребляется и создаётся 30-дневная session.
   Ответ 201: `{"token":"mge_...","token_type":"Bearer","expires_at":"..."}`.
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

Реализован API `/v1/imports` вместо прежнего проекта `/v1/captures` (ADR-0011).
Контракт, примеры, retry semantics и лимиты — [Extension API](extension-api.md),
машиночитаемая схема — [OpenAPI 3.1.1](openapi.json). HTTP wire DTOs в
extension/src/core/contracts.ts. Основа runtime расширения реализована (ADR-0012):
pairing, popup и отправка чанков. Yandex DOM adapter реализован отдельно (ADR-0013),
его capture ещё не подключён к popup/outbox; Spotify/VK остаются заглушками.

Import появляется в collecting. Чанки атомарно записывают tracks/items/receipts;
complete проверяет последовательность и только затем создаёт jobs. partial/complete
фиксируются отдельно от execution state. CaptureTransport/Ingestion из старого
проекта заменены ImportTransport/UploadStore. Никаких credentials стриминга.

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
