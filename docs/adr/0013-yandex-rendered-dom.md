# ADR-0013: Yandex Music — rendered DOM, виртуализация и проверяемый конец

Статус: принято. Уточняет ADR-0001/0006/0012.

## Решение

YandexAdapter реализует MusicSourceAdapter для favorites, playlist и album.
Selectors находятся только в sources/yandex/selectors.ts, route/metadata/track
parsing и collection traversal разделены. Общие DOM helpers не знают о сервисе.
Spotify и VK остаются stubs. Реальных запросов к сервису, доступа к cookies,
web storage, hydration, fetch/XHR или application stores в адаптере нет.

Собираются только строки в viewport, с учётом скрытых родителей и clipping
контейнеров. Текст скрытых descendants не читается. Player/recommendations
исключаются. Track ID извлекается исключительно из отображаемой ссылки
/track/ID или /album/ID/track/ID. Query/fragment удаляются локально;
userinfo, посторонние hosts и произвольные пути не экспортируются.
URL коллекции возвращается в локальном CollectionMetadata.url; это не новое
поле backend DTO. Для альбома допустим fallback из видимого заголовка страницы.

Коллектор начинает с верха ближайшего scroll container, проходит его шагами
70% viewport, ожидает MutationObserver/settling и отдаёт AsyncGenerator batches
до 200 tracks с запасом 1 KiB под HTTP envelope в лимите 512 KiB. Consumer
обеспечивает backpressure: до следующего next() прокрутка не продолжается.
В памяти — текущий batch и bounded Map ключ → metadata digest (до maxTracks,
не более 100 000), а не вся metadata библиотеки. DOM nodes между проходами
не используются как идентичность: виртуализатор может переиспользовать их.

Без source ID используется capture-local SHA-256 от NFKC/whitespace-normalized
полей title, artists (sorted), album, exact duration и version. Это НЕ canonical
fingerprint v1 backend и не acoustic identity. Metadata-only capture всегда
partial: одинаковые описания различных записей неразличимы. Stable ID с разными
metadata также делает capture partial. Повтор одного ID имеет set semantics;
повторные playlist occurrences могут не совпасть со счётчиком — тогда partial.

`complete` требует нескольких спокойных проходов у нижней границы, отсутствия
видимого loading indicator/неполных строк/неустойчивой идентичности и точного
совпадения с отображаемым числом треков. Без счётчика необходим явный видимый
маркер конца. Одна тишина, scrollHeight или timeout дают только partial.
Изменение URL/path, collection root/title или уже известного счётчика даёт
partial/dom_changed. AbortSignal завершает partial/user_stopped, отключает
observer/timer, новых scroll не выполняет. maxTracks/maxSteps ограничивают сбор.
Callback onProgress сообщает count, optional expected, phase и final summary.

## Ограничения и проверка

HTML fixtures синтетические, обезличенные, описывают legacy d-track и CSS-module
CommonTrack разметку. Это не снятые live snapshots и не подтверждение актуальных
selectors всех rollout Яндекса. jsdom не выполняет layout; тесты явно задают
viewport geometry и переиспользуют DOM nodes. Scripts/resources отключены,
network/storage/cookie access в fixtures бросает исключение. Ручная проверка
живого rendered DOM остаётся необходимой перед заявлением live compatibility.

В этом этапе готов source adapter, зарегистрированный в page.info. Popup всё
ещё запускает только demo ingestion. Content-to-background DOM outbox transport,
запуск реального capture из popup, его durable ACK/resume после reload остаются
отдельной задачей; нельзя выдавать callback progress за уже подключённый UI.

Источники browser primitives:
- [MutationObserver](https://developer.mozilla.org/en-US/docs/Web/API/MutationObserver)
- [scrollHeight и погрешность проверки нижней границы](https://developer.mozilla.org/en-US/docs/Web/API/Element/scrollHeight)
