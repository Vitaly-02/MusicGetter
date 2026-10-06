# ADR-0014: Spotify Web — изолированный rendered DOM adapter

Статус: принято. Уточняет ADR-0001/0006/0012; следует структуре ADR-0013.

## Решение

SpotifyAdapter поддерживает Liked Songs (/collection/tracks), playlist и album
на open.spotify.com, включая локализованный префикс /intl-xx/. Адаптер читает
только отображаемую разметку: заголовок коллекции, основной tracklist и видимые
строки. Идентификатор берётся только из отображаемой ссылки /track/:id; query и
fragment удаляются. Не читаются data-uri, React props, JSON scripts, cookies,
storage, network responses. Никаких запросов или перехвата сети Spotify нет.

Selectors, route matching, metadata parsing, traversal/end policy находятся
в sources/spotify. Адаптер не импортирует Yandex или VK. Общими остаются core
DTO/visibility/wait helpers. Небольшой collector локален, чтобы изменения Spotify
virtualization/end semantics не меняли работающий Yandex adapter автоматически.

Title сохраняется с live/remaster/version qualifiers. Artists берутся из
отображаемых ссылок /artist/:id либо явных текстовых элементов строки, album —
из album link/text или видимого заголовка album page. Duration — точный m:ss/h:mm:ss
из duration element или последней gridcell; неизвестное значение опускается.
Локальные/недоступные аудиозаписи с доступной metadata допускаются; отсутствие ID
не блокирует сбор. Неполные строки пропускаются с partial итогом.

Основной список отделён от player, recommendations, queue и других grids.
aria-rowindex не используется как track position: заголовки/disc separators
смещают нумерацию. Есть aria-posinset — используем его; иначе позиция означает
порядок первого наблюдения. aria-rowcount также не считается числом треков;
используется только явно отображаемый song/track counter. Counter parser распознаёт
точные английские/русские числа; неизвестная локаль/сокращение означает unknown,
а не угаданную полноту.

Коллектор проходит список с начала, шаг 70% scroll viewport, ждёт DOM settling,
учитывает увеличение scrollHeight при infinite loading. AsyncGenerator даёт
backpressure; batch <=200 tracks и <=512 KiB с запасом под HTTP envelope.
Map bounded (maxTracks <=100 000) хранит ID/digest, а не всю metadata библиотеки.
Повторные ID дедуплицируются, разные ID с одинаковыми metadata остаются разными.
Без ID — capture-local metadata SHA-256, результат всегда partial. Fingerprint
backend v1 не изменяется. Несовпавшая metadata одного ID даёт partial.

Полнота, abort/progress и лимиты следуют ADR-0013: несколько спокойных проходов
внизу, отсутствие loading/неполных строк/неустойчивой идентичности, точный видимый
счётчик либо явный видимый маркер конца. Cap не позволяет complete при пропущенных
строках. Unknown/stalled end — partial, смена route/root/list/title/count —
dom_changed, AbortSignal — user_stopped. Observer и timers отключаются при abort.

## Проверки и ограничения

Обезличенные fixtures синтетические; jsdom моделирует геометрию явно. Tests
покрывают три вида страниц, virtualized node reuse, infinite growth, backpressure,
progress/cancel, дедуп, SPA changes, hidden/clipped data, unsafe links, count/end
ambiguity, byte/count budgets и cleanup. Source network/storage/cookie reads
в tests запрещены. Live Spotify selectors не проверены авторизованной сессией.

Как у Yandex, готов source adapter и page.info detection. Popup всё ещё запускает
только demo ingestion: content-to-background capture/ACK/resume transport ещё
не подключён. Полный импорт означает возможность обхода доступной DOM коллекции,
а не обещание экспорта недоступных или не загруженных данных и доставки в destination.

Основание для осторожной трактовки grid counts:
[MDN aria-rowcount](https://developer.mozilla.org/en-US/docs/Web/Accessibility/ARIA/Reference/Attributes/aria-rowcount).
