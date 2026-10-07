# Browser extension API v1

Контракт: [OpenAPI 3.1.1](openapi.json). Реализованы все маршруты ниже; обработчик
импорта/matcher и музыкальный destination пока не исполняются. `/complete`
ставит jobs в PostgreSQL, готовые для будущего worker.

| Метод и путь | Назначение |
|---|---|
| POST /v1/pair/claim | Одноразовая привязка кодом из `/connect` |
| GET /v1/me | Текущий пользователь и extension session |
| GET /v1/destinations | Сохранённые коллекции текущего пользователя, cursor/limit |
| POST /v1/imports | Создать import в state created, client_request_id обязателен |
| POST /v1/imports/{id}/tracks | Атомарно принять чанк, idempotency_key обязателен |
| POST /v1/imports/{id}/complete | Проверить диапазон чанков и завершить сбор |
| POST /v1/imports/{id}/cancel | Идемпотентно отменить импорт |
| GET /v1/imports/{id} | Состояние и агрегированный прогресс |

## Авторизация и Origin

В личном чате управляющего бота выполните `/connect`, затем отправьте:

```http
POST /v1/pair/claim
Content-Type: application/json

{"code":"<код из бота>"}
```

Ответ 201 содержит `token`, `token_type: "Bearer"`, `expires_at`. Далее каждый
запрос использует `Authorization: Bearer <MusicGetter token>`. Session действует
30 дней; отзыв через `/settings` проверяется по БД на каждом запросе. Plaintext
не хранится. Потеря ответа claim требует нового `/connect`, а не получения того
же token повторно. Cookies не используются и отклоняются.

`EXTENSION_ORIGINS` — список через запятую из точных origins, например
`chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`. Укажите реальный ID вашего
расширения; для Firefox — `moz-extension://<UUID>` текущего установленного экземпляра.
Пустой список отклоняет все запросы с Origin. Запросы без Origin допускаются для
extension service worker/локального клиента, но требуют Bearer. Preflight OPTIONS
разрешает только GET/POST и Authorization, Content-Type, Idempotency-Key, X-Request-ID.
CORS не выдаёт `Access-Control-Allow-Credentials`; wildcard и web origins запрещены.

## Создание и чанки

Сначала GET /v1/destinations?limit=50. `items` содержит id, connection_id, adapter,
kind, title. `next_cursor` передаётся как cursor следующей страницы; последняя
может быть пустой. Лимит 1–100. Пока destination adapter/provisioning не реализован,
новый пользователь увидит пустой список; API не создаёт фиктивную коллекцию.

```json
{
  "client_request_id": "create-client-unique-key",
  "source": {
    "service": "spotify",
    "profile_key": "personal",
    "collection_key": "liked-songs",
    "kind": "favorites",
    "title": "Liked songs",
    "provisional": false
  },
  "destination_collection_id": "<UUID из /v1/destinations>"
}
```

POST /v1/imports принимает этот JSON (до 16 KiB). `profile_key` — локальный
пользовательский идентификатор профиля, не streaming account credential. Если DOM
не предоставляет стабильный collection key, extension генерирует и сохраняет
локальный ключ с provisional=true. Повтор того же client_request_id и payload
вернёт прежний id (200, replay=true), первый ответ — 201, replay=false.
Тот же ключ с другим payload — 409. Owner определяется только по Bearer.

POST /v1/imports/{id}/tracks, максимум 200 треков и 512 KiB на запрос:

```json
{
  "schema_version": 1,
  "idempotency_key": "chunk-0-client-unique-key",
  "sequence": 0,
  "tracks": [
    {"title": "Song", "artists": ["Artist"], "album": "Album", "duration_ms": 210000, "position": 0}
  ]
}
```

Source/key/url для трека необязательны: `source_track_key`, `source_url`. Отсутствие
стабильного ID использует metadata fingerprint v1. source определяется импортом,
его нельзя переопределить в track. Допустимы только разрешённые rendered links
того же source без query, fragment, userinfo. Никаких API/cookies/access_token,
HTML или hidden payloads. Неизвестные поля, включая вложенные, отклоняются.

Квитанция: sequence, idempotency_key, received (наблюдений), added (новых уникальных
items), replay. Храните неизменённый чанк в outbox до ACK. При timeout отправьте
тот же key/sequence/body: дубли не создаются, received/added остаются исходными,
replay=true. Форматирование JSON и порядок полей не влияют; metadata и порядок
tracks влияют. Не переиспользуйте sequence с другим key или key с другим payload.
Необязательный Idempotency-Key header должен совпадать с ключом в body.

Можно отправлять чанки вне порядка, рекомендуем до 4 in flight. На import —
максимум 100 000 observations, 10 000 chunks. Номера начинаются с 0. Receipt replay
работает даже после завершения/отмены, подтверждая прежний ACK, но не оживляет import.
GET /v1/imports/{id} возвращает received_chunks, received_observations,
contiguous_through (-1 до sequence 0), total_tracks и счётчики исполнения.
Количество observations включает повторы; total_tracks учитывает дедупликацию.

## Завершение и отмена

POST /v1/imports/{id}/complete:

```json
{"last_sequence":0,"observed_count":1,"completeness":"partial","reason":"user_stopped"}
```

Сервер требует все chunks 0..last_sequence, отсутствие лишних chunks и точный
observed_count. Пустой capture: last_sequence=-1, observed_count=0. complete допустим
только с visible_end_confirmed; partial — user_stopped, dom_changed, unknown_end.
Не считать таймаут или неподвижную страницу доказательством полного обхода DOM.
До seal jobs отсутствуют; seal и создание jobs атомарны. Неизменённый complete
можно повторять после потери ACK; изменённый body даёт 409. Header Idempotency-Key
для complete не используется. Ответ показывает актуальный import state.

POST /v1/imports/{id}/cancel принимает пустое тело или `{}`. Повторная отмена
безопасна. Новые чанки отвергаются, pending items отменяются, ready jobs останавливаются.
Уже выполненные внешние действия не отменяются; uncertain/leased операции остаются
за будущим worker/reconciliation. Успешно завершённый import отменить нельзя (409).

## Ошибки и ограничение нагрузки

```json
{"error":{"code":"conflict","message":"Idempotency payload, import state or chunk range conflicts"},"requestId":"..."}
```

- 400 — malformed JSON, duplicate/unknown/case-mismatched fields, forbidden credentials.
- 401 — MusicGetter token/code отсутствует, неверен, истёк или отозван.
- 403 — Origin/preflight не разрешён; 404 — отсутствующий или чужой ресурс.
- 405 — неверный метод; 409 — несовместимый retry/state или пропуск чанков.
- 413 — body превышает лимит; 415 — нужен uncompressed UTF-8 application/json.
- 422 — неверные metadata/счётчики, >200 tracks или превышение import quota.
- 429 — rate limit, соблюдайте Retry-After; 503 — временная ошибка storage.

Сеть/503: retry с тем же ключом и body, bounded exponential backoff с jitter.
429: выждать Retry-After. 409/422 требуют исправления состояния/данных; не повторять
бесконечно. 401 требует перепривязки. Error messages не содержат raw SQL, input,
source credentials или токенов. Ответы Cache-Control: no-store.

Лимиты на процесс: API_IP_PER_MINUTE=120, API_OWNER_PER_MINUTE=60,
API_CLAIM_PER_MINUTE=5, fixed-minute windows, bounded map 10 000 keys. Перезапуск
сбрасывает лимиты; нескольких replicas пока нет. Все sessions владельца делят
owner limit, legacy pairing alias делит claim limit. X-Forwarded-For не доверяется:
за reverse proxy без отдельного доверенного слоя клиенты делят IP quota. Production
нуждается в HTTPS termination и общем edge limiter при горизонтальном масштабировании.

Совместимость: /v1/pairings/redeem и /v1/extension/session сохранены как deprecated
aliases с прежними JSON shapes, но теми же проверками и лимитами. Бизнес-маршрутов
/v1/captures нет: они были только проектом и заменены текущим контрактом.
