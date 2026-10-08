# Destination adapters

Реализованы Go-порты, reference `memory`, fault-injection `fake` и PostgreSQL
album fallback (ADR-0019). Реального музыкального Telegram adapter нет;
`cmd/worker` сохраняет пустой registry. Управляющий bot не становится клиентом
произвольного другого бота. Протокол и разрешения будущей интеграции должны быть
подтверждены отдельно.

## Порты

Все вызовы привязаны к одной `DestinationConnection`, определённой trusted factory
в `importer.Registry`. Owner/connection проверяются до получения adapter; внешние
ID не являются авторизацией. Domain не импортирует destination или Telegram SDK.

| Порт | Методы |
|---|---|
| `Destination` | `Capabilities`, `EnsureTrack` |
| `TrackSearcher` | `SearchTracks` |
| `Favorites` | `GetFavorites`, `AddToFavorites` |
| `Playlists` | `ListPlaylists`, `FindPlaylist`, `CreatePlaylist`, `GetPlaylistTracks`, `AddTracksToPlaylist` |
| `Albums` (optional) | `ListAlbums`, `CreateAlbum`, `GetAlbumTracks`, `AddTracksToAlbum` |
| `MembershipReader` | `Contains` |
| `OperationReconciler` | `Reconcile` |

Контракты находятся в `internal/destination/{contracts,collections,capabilities}.go`.
Основной интерфейс остаётся небольшим; pipeline пишет через `EnsureTrack` и может
искать непосредственно через `TrackSearcher`, без отдельного Catalog wrapper.
Добавление adapter требует регистрации factory, без изменения pipeline.

Flags: `supports_search`, `supports_favorites`, `supports_playlists`,
`supports_album_collections`, `supports_bulk_add`, `supports_membership_lookup`.
`MaxBulkSize` ограничен 200; без bulk Add-методы принимают один item.
Отдельные гарантии: `AtomicEnsureMembership`, `NativeIdempotency`,
`ReconcileOperations`, `IdempotentCreatePlaylist`. Feature flag не доказывает
безопасность записи. `CheckAdapter` проверяет наличие объявленных Go-портов;
factory должна вызвать его, а реальный adapter — пройти contract tests и доказать
семантику внешнего сервиса. Одного read-before-add недостаточно для atomic ensure.

Чтения возвращают bounded pages (1–200), opaque cursor — только для того же
account/операции/фильтра. Это keyset обход, не snapshot изменяемой библиотеки.
`FindPlaylist` ищет точное display name и возвращает все совпадения страницами:
одинаковое имя не является identity. Пустой `NextCursor` означает конец обхода.
Все методы принимают context; transport errors должны быть очищены от токенов/URL.

## Записи и неопределённый результат

Каждый add имеет свой постоянный `OperationKey`. Повтор ключа с другим payload
возвращает конфликт. Atomic ensure обеспечивает set semantics даже для заранее
существующего трека и конкурирующих вызовов с разными ключами. `Effect` различает
applied/already_present/pending/unknown/rejected. Только первые два подтверждают
membership. Timeout, отмена context и ошибка transport не доказывают отсутствие
эффекта. Bulk может вернуть частичные per-item результаты вместе с ошибкой;
неизвестные items требуют reconciliation или доказуемо безопасного ensure.

Pipeline пока сохраняет один job/ensure на item: bulk-порт реализован и доступен,
но coalescing worker jobs не добавлен. Это сохраняет существующие fencing,
checkpoints и per-item retries. Не объявляется exactly-once для неизвестного бота.

## Album → playlist

Если **запрошенный destination target имеет kind album**, а adapter не поддерживает
album collections, pipeline создаёт playlist `Artist — Album`. Явно выбранный
favorites/playlist не переопределяется. Нужны supports_playlists и
IdempotentCreatePlaylist: один ключ обязан сохранять immutable request/result
на весь срок жизни коллекции, включая concurrency и потерю ответа. Без этой
гарантии item завершается `unsupported_target`, remote create не вызывается.

До I/O PostgreSQL сохраняет `destination_target_bindings`: requested target,
owner+connection, creation key и неизменное имя. Artist берётся из самого раннего
captured item по position/id, album — из его metadata, затем из source album title
или названия requested target. Album artist не выдумывается; для сборников это
исполнители первого трека. Имя ограничивается 1024 Unicode symbols и не служит
идентификатором. После ACK binding сохраняет playlist ID. Lease/fencing проверяются
в каждой транзакции, сетевых вызовов внутри транзакции нет.

`imports.destination_collection_id` остаётся неизменным для upload replay;
`resolved_destination_collection_id` указывает фактическую коллекцию для ledger.
Повторные импорты того же logical target используют binding, даже если позже
adapter начнёт поддерживать native albums. Pending creation также не забывается.
Разные logical targets с одинаковым названием не объединяются автоматически.

После падения процесса ключ остаётся в БД, новый worker повторяет безопасный create.
Отмена останавливает дальнейшую доставку, но не удаляет создание/unknown intent;
плейлист может остаться пустым, включая случай отсутствия совпадений.
Новый API provisioning destination targets не добавлен: для fallback нужна заранее
сохранённая logical album collection. Пустой список destinations нового пользователя
не заполняется fake-данными.

## Reference implementations и проверки

`memory.New(Config)` создаёт isolated account с seed catalog/collections, mutex,
keyset pagination, immutable operation receipts и atomic membership sets.
Все metadata возвращаются копиями. Read-page temporary memory ограничена размером
страницы; сам reference store хранит весь seed catalog/ledger в RAM и не durable.
Поиск reference store — простой AND по нормализованным словам, оценка кандидатов
остаётся ответственностью matcher.

`fake.New(memoryDestination)` добавляет `FailNext(operation, Fault{AfterApply:true})`
и счётчики вызовов. Можно моделировать сбой до эффекта и потерю ACK после эффекта.
Пересоздание fake поверх прежнего memory моделирует перезапуск клиента при
сохранившемся внешнем аккаунте. Эти реализации не подключены к production registry.

Повторно используемый `destinationtest.Run(t, factory)` проверяет порты,
capabilities, поиск, pagination, set semantics, immutable keys, конкурентное
создание, одинаковые имена, bulk receipts, optional albums и cancellation.
Factory выдаёт свежий изолированный тестовый account с favorites ID `favorites`
и seed tracks `a`/`b`, artist `Artist`; использовать пользовательскую библиотеку
для этих тестов нельзя.

```sh
make test
make lint
TEST_DATABASE_URL='postgres://musicgetter:musicgetter@127.0.0.1:5432/musicgetter?sslmode=disable' make test-integration
```

Integration suite запускает реальный HTTP handler с pairing/Bearer, загрузкой
chunks, complete, worker queue, matcher и fake destination. Покрыты все kinds,
повторный chunk/import, lost ACK create/add, shared creation intent, пересоздание
pipeline, изменение capabilities, fencing, owner scope и rollback guard.
