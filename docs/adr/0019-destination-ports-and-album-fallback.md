# ADR-0019: Destination ports и durable album fallback

Статус: принято. Дата: 2026-10-08. Дополняет ADR-0005 и ADR-0017.

## Контекст

Нужны операции поиска, favorites и коллекций без привязки pipeline к неизвестному
музыкальному боту. Не все destinations представляют album как коллекцию. Повторный
CreatePlaylist после потерянного ответа может создавать дубли ещё до add-track.

## Решение

Сохранить Destination(Capabilities, EnsureTrack), добавить отдельные Favorites,
Playlists, optional Albums и оставить TrackSearcher/MembershipReader/Reconciler.
Capabilities отделяют доступность функций от гарантий внешних эффектов; reads и
bulk bounded, bulk имеет отдельный ключ/result на item. FindPlaylist возвращает
страницы совпадений: display name не является identity.

Album target без native support разрешать в playlist `Artist — Album` только при
подтверждённом idempotent create. Миграция 00008 сохраняет immutable creation key/name
до I/O и owner-scoped requested→resolved binding. Imports сохраняют original target
для replay, membership использует effective target. Существующее решение сохраняется
при изменении capabilities. Произвольный title lookup не заменяет durable binding.

Memory — account-scoped reference implementation, fake — fault injection поверх неё.
Обе только для тестов/локального harness, production registry пуст. Новый adapter
подключается trusted factory; contract suite дополняется проверкой гарантий сервиса.

## Альтернативы и последствия

Giant interface обязал бы каждый adapter притворяться поддерживающим albums.
Слепой retry создания и выбор первого совпадения по имени отвергнуты. Без гарантии
идемпотентного create автоматический fallback отключён. Отдельный reconciliation
протокол создания можно добавить позднее, когда известен реальный destination.

First-track artists используются для display name; у сборников это не album artist.
Потребуется provisioning logical targets при подключении реального adapter.
Bulk coalescing worker jobs отложен, текущая доставка остаётся per-item.
Удаление/rename/reorder и компенсационное удаление пустого playlist не добавлены.
Rollback 00008 запрещён при наличии creation ledger: забывать ключи небезопасно,
использовать forward repair. Условия миграции — docs/database.md.
