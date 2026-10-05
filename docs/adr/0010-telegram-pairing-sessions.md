# ADR-0010: Telegram bot, bot-issued pairing и extension sessions

Статус: принято. Дата: 2026-10-05. Уточняет ADR-0005 и заменяет проект
extension-first pairing из docs/protocol.md по требованию `/connect` в боте.

## Решение

Наш управляющий бот использует официальный [Telegram Bot API](https://core.telegram.org/bots/api):
getMe, getUpdates, sendMessage, answerCallbackQuery, setMyCommands. Адаптер на
net/http, без SDK. TelegramService — узкий outbound port для handlers, polling
имеет отдельные интерфейсы. Domain не зависит от Telegram. Музыкальный destination
не вызывается; `/playlists` читает только сохранённые в нашей БД коллекции.

Команды разрешены только в private chat с chat.id == from.id, от пользователей,
не ботов. Telegram identity приходит исключительно из getUpdates по TLS; публичного
HTTP endpoint приёма Telegram updates нет. Callback actions — фиксированные команды
либо UUID для owner-scoped чтения. Ввод пользователя не задаёт owner. Ответы plain
text, previews отключены, длинные названия сокращаются, страницы по 10 записей.

`/connect` генерирует crypto/rand 128 bits, Base32 без padding (26 символов).
БД хранит SHA-256 кода, owner и ровно 5 минут TTL по серверному времени БД.
Предыдущий bot-issued код того же пользователя отзывается. Код — bearer secret:
владение кодом даёт возможность привязки, поэтому выдаётся только в личном чате
с protect_content. Это осознанное отличие от старого verifier flow; защита от
передачи кода третьему лицу не обещается. Короткий цифровой PIN отвергнут из-за
онлайн-перебора. Старые challenge pairings сохраняют отдельный repository contract,
но новых HTTP handlers для прежнего flow нет и bot-issued redeem их не принимает.

POST /v1/pairings/redeem атомарно погашает код и создаёт сессию. Token — 256 random
bits, base64url, prefix mge_, TTL 30 дней без refresh. Возвращается только один раз,
БД хранит SHA-256, не plaintext. Медленный password hash не требуется для случайных
256-bit credentials. Потеря HTTP-ответа требует нового `/connect`, replay не выдаёт
тот же token. Authenticate проверяет expires/revoked в БД каждый раз, без кеша.
Сейчас единственный защищённый HTTP read — GET /v1/extension/session. Будущие
бизнес-маршруты обязаны использовать эту проверку и owner-scoped authorization.

Issue/redeem/revoke сериализуются на users row FOR UPDATE. Отзыв всех сессий из
/settings также отзывает pending codes. Конкурентный redeem либо предшествует
revoke и его сессия отзывается, либо видит отозванный код. Начатый до отзыва запрос
не отменяется ретроактивно. Все изменения одной операции — одна транзакция.

## Доставка команд и сбои

Один long-polling процесс на bot ID; отдельное PostgreSQL connection держит advisory
lock. SIGINT/SIGTERM отменяет long poll и DB operations, connection закрывается.
Webhook не настраивается и не удаляется автоматически; существующий webhook или
другой poller даёт conflict, процесс завершается. Retry polling ограничен backoff,
учитывает retry_after; ошибки отправки сообщений автоматически не повторяются.

PRIMARY KEY(bot_id,update_id) дедуплицирует доставку между рестартами. Claim фиксируется
перед handler. Это at-most-once попытка выполнения: crash или DB/send failure после
claim может потерять команду/ответ. Пользователь повторяет команду новым сообщением;
для потерянного кода новый `/connect` отменяет предыдущий. Exactly-once сообщения
не обещаются: у sendMessage нет нашего idempotency key. Telegram update payloads,
ответы и credentials не сохраняются в dedup table и не логируются. Пока записи
dedup сохраняются без автоматического удаления; retention — отдельная ops-задача.

## Миграция и проверки

00005 добавляет extension_sessions, telegram_updates, revoked_at, допускает NULL
challenge только с owner, добавляет индекс хронологических imports. ALTER TABLE
берёт краткий ACCESS EXCLUSIVE; CREATE INDEX на imports блокирует writes при
построении. Применять coordinated deployment; на крупной работающей БД индекс
потребует отдельной CONCURRENTLY migration. Readiness требует schema version 5.
Down уничтожает sessions/dedup и bot-issued или revoked pairings; не оживляет
отозванные legacy-коды. Остановка writers и backup обязательны для рабочего отката.

Тесты: команды/inline/private ownership, safe errors, HTTP DTO/context/auth,
Bot API transport, repeat updates/panic, TTL, SHA-256 storage, concurrent redeem,
rollback session insert, revoke race, cross-owner reads, migrations up/down.
TLS termination и ограничение нагрузки на публичный API настраиваются при deployment;
локальный Compose публикует HTTP только на loopback.
