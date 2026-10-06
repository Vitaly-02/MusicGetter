# ADR-0015: VK Music — rendered DOM и коллекции в диалогах

Статус: принято. Следует ADR-0001/0006/0013/0014.

## Решение

VKAdapter реализует существующий MusicSourceAdapter без расширения domain или
общего interface. Selectors, URL navigation, metadata и collection traversal
изолированы в sources/vk. Из других source adapters нет imports. Общие DOM
visibility/wait helpers и DTO берутся из core.

Поддержаны fixture-контракты legacy audio_row и modern music-track UI:
- /audios:owner — сохранённые треки; /music — только при видимом заголовке или
  активной вкладке «Моя музыка»/«Сохранённые треки» (либо английском эквиваленте).
- /music/playlist/:owner_:id, /audio_playlist:owner_:id — playlist.
- /music/album/:owner_:id — album, только с отображаемым track list.
  Playlist UI с явной видимой меткой «Альбом» также может представлять album.
  Обложка/название без списка не поддерживаются; название само по себе не
  доказывает album kind.

Точные hosts: vk.com и music.vk.com, HTTPS. User-facing ?z=audio_playlist... может
открывать коллекцию в dialog поверх библиотеки. Из видимого URL извлекается только
numeric owner/id, optional share/access suffix отбрасывается. Backend получает
canonical collection path без query/fragment/suffix. Dialog имеет приоритет над
фоном; неизвестный dialog блокирует сбор, чтобы не импортировать чужой список.
Query-only смена collection key прекращает capture как dom_changed. Так VK modal
navigation остаётся локальной деталью adapter, без VK-specific concepts в domain.

Track key извлекается только из видимой ссылки /audio:owner_:id либо
/music/track/:owner_:id. Query/fragment удаляются, credentials/посторонние hosts/
произвольные URLs не экспортируются. data-audio, data-full-id, React/internal state,
JSON scripts, source storage и cookies не читаются. Source network отсутствует:
нет VK API, HTTP request/response анализа, XHR interception или запроса media URL.

Artists сохраняются отдельными элементами, если UI имеет отдельные ссылки;
plain-text performers остаётся одним значением без угадывания разделителей.
Optional album, version, duration извлекаются из видимого текста. Для album
заголовок коллекции служит fallback album title. Нет ID — metadata fallback с
partial итогом, как в ADR-0013/0014; canonical backend fingerprint не меняется.

Collection traversal использует bounded AsyncGenerator batches (200 tracks,
512 KiB минус envelope reserve), compact bounded dedup Map, overlapping scroll,
DOM settling, progress callback и AbortSignal. Infinite loading увеличивает
scrollHeight и продолжает проход. Тишина или bottom сами по себе не доказывают
complete: нужны count/end evidence и отсутствие ambiguity/loading/missing metadata.
Navigation/root/list/title/count/kind changes дают partial/dom_changed.

## Проверки и границы

Fixtures синтетические, обезличенные; scripts/resources отключены. Тесты запрещают
чтение network/cookies/storage/data-audio/data-full-id и моделируют jsdom layout.
Проверяется модальный playlist поверх библиотеки, album без ID/без списка,
скрытые поля, ссылочные credentials, query-only navigation, cancellation,
backpressure, infinite growth и 1 200 виртуальных треков при трёх DOM rows.
Это correctness test, не performance benchmark или доказательство live compatibility.
Selectors текущего авторизованного VK UI вручную ещё не проверены.

Адаптер зарегистрирован для page.info, как Spotify/Yandex. Запуск реального capture
из popup, transport в durable outbox и resume после reload остаются отдельным
этапом; существующая кнопка запускает demo ingestion. Музыкальный destination и
worker не добавляются этой задачей.
