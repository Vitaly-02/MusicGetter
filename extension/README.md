# MusicGetter browser extension

Рабочая основа Manifest V3 / TypeScript: popup, pairing, reconnect/logout,
определение source, API transport, durable очередь чанков, progress и cancel.
Chrome/Chromium 120+ — основная цель. Отдельная сборка Firefox 128+ использует
background scripts вместо service worker; интерфейсы core/storage одинаковы.
Firefox runtime ещё не сертифицирован ручными проверками.

## Сборка и установка

Нужен Node.js 20+ и npm. Из корня проекта:

```sh
cd extension
npm ci
npm run typecheck
npm test
npm run build
```

Chrome: откройте `chrome://extensions`, включите «Режим разработчика», выберите
«Загрузить распакованное расширение» → `extension/dist/chrome`. Откройте popup
кнопкой расширения: открытие popup.html как обычной вкладки намеренно не даёт
доступ к командам управления.

Backend по умолчанию — `http://127.0.0.1:8080`. Другой backend задаётся при сборке:

```sh
MUSICGETTER_BACKEND_URL=https://musicgetter.example.com npm run build
npm run build:firefox
```

Переменную нужно задавать для каждой нужной сборки. Разрешён HTTPS origin без
path/query/userinfo; HTTP только localhost/127.0.0.1 для разработки. Адреса
стримингов в качестве backend отклоняются. Браузерное optional permission выдаётся
только этому host по явному нажатию «Подключить». Запросы ограничены точным
скомпилированным origin (включая порт) и CSP; redirects запрещены.

Firefox: `about:debugging` → «Этот Firefox» → «Загрузить временное дополнение» →
`extension/dist/firefox/manifest.json`. Это локальная development-сборка, не
готовый пакет для публикации в AMO. После временной установки origin Firefox
может отличаться между профилями/установками.

## Подключение

1. Запустите backend и управляющего бота по корневому README.
2. В `.env` backend добавьте свой origin в `EXTENSION_ORIGINS`, например
   `chrome-extension://<ID из chrome://extensions>`, затем пересоздайте backend.
   Для Firefox нужен `moz-extension://<UUID установки>`, не addon ID.
3. В личном чате бота вызовите `/connect`, вставьте код в popup и разрешите
   подключение к вашему backend.
4. «Проверить связь» вызывает `/v1/me`; при истечении/отзыве нужен новый `/connect`.

Pairing не повторяет одноразовый claim автоматически. Если ответ потерян или
последующий /me недоступен, запросите новый код. Секрет не попадает в сообщения
content script, страницу, URL, логи или sync storage. Token хранится в IndexedDB
origin самого расширения; content scripts работают в origin страницы и не имеют
доступа к этой БД. Popup получает только безопасную проекцию статуса.
Это изоляция браузера, а не шифрование от пользователя ОС/доступа к профилю.

«Выйти» удаляет token и локальную задачу, останавливает локальные запросы. Уже
принятый сервером import не отменяется выходом: используйте «Отменить импорт»
до выхода. Для отзыва всех sessions используйте `/settings` управляющего бота.
Смена аккаунта не может возобновить очередь другого владельца.

## Источники и демонстрация

Yandex, Spotify и VK DOM adapters реализованы и проверены
на синтетических fixtures, см. разделы ниже. Stub возвращает supported=false,
metadata=null и явную unsupported ошибку вместо фиктивного успешного сбора.
Никаких source API, cookies, webRequest, XHR interception или hidden stores.

Отдельный opt-in «Проверка переноса» отправляет **450 синтетических треков**
(200 + 200 + 50). Требуется подключение и существующая destination collection
в backend. Новый пользователь может увидеть пустой список: provisioning реального
музыкального destination ещё не реализован. Демо не создаёт фиктивную destination
и не выдаётся за Spotify-библиотеку. На backend source=spotify используется только
для synthetic fixture; source profile отделён префиксом `demo:`.

Popup показывает приём чанков и агрегированное состояние backend. Завершение
загрузки означает queued, а не доставку музыки: import worker ещё не подключён.
«Статус» обновляет серверное состояние; «Отменить импорт» выполняет server cancel.

## Очередь и lifecycle

Одна активная задача, один immutable pending chunk, 200 tracks / 512 KiB maximum.
Чанк сохраняется до POST и удаляется только после проверенного ACK. Ключи create
и chunk сохраняются между рестартами service worker. Потеря ответа вызывает
повтор того же payload; received/added прибавляются только после ACK.

Network/5xx/429: exponential backoff + jitter, Retry-After, максимум 8 попыток,
затем явное «Повторить». Deadline HTTP — 15 секунд; redirects/cookies запрещены.
401 требует новой привязки; 409/validation ошибки приостанавливают задачу.
Отмена сохраняется до запроса и имеет приоритет над отправкой. При потерянном
create ACK сначала восстанавливается import ID тем же client_request_id, затем
отправляется cancel. При offline отмена остаётся ожидающей, не показывается успешной.

Chrome alarms будят worker раз в минуту; popup events продолжают работу чаще.
За одно пробуждение выполняется до четырёх шагов. После закрытия popup очередь
продолжится по alarms, браузер может задерживать пробуждения. Искусственного
keepalive и бесконечных retry loops нет. Неподтверждённая metadata ограничена одним
пакетом; полная библиотека не загружается в RAM.

## Source adapter

`src/core/adapter.ts` определяет `MusicSourceAdapter`, `PageContext`,
`CollectionMetadata`, `Track`, `Disposable`. Методы: detectPage,
getCollectionMetadata, collectVisibleTracks, collectAllTracks, observe и optional selectionRows.
collectAllTracks возвращает **AsyncGenerator ограниченных пакетов**, а не
Promise всей библиотеки — для десятков тысяч треков.

Каждый реальный adapter использует только видимый DOM и разрешённую прокрутку.
DOM capture подключён к background через scoped selection RPC (ADR-0016).
Consumer сохраняет выбранные metadata в extension-origin IndexedDB; после freeze
keyset producer передаёт batches в durable outbox. UI после reload требует нового
открытия режима, а queued upload переживает restart background.

Capture и upload проверяются на fixtures и fake transport без обращений к стримингам.

## Проверки

```sh
npm ci
npm run typecheck
npm test
npm run build
npm run build:firefox
```

Node test runner + esbuild, fake-indexeddb только для tests. Проверки core:
byte/count chunk bounds, unsafe metadata, lost ACK/restart, concurrency, cancel,
retry budget, owner/origin isolation, token projection, auth errors, IndexedDB
transactions/logout. Dependencies — только dev/build; runtime dependencies нет.
Собранные dist, node_modules и .test-build исключены из Git.

## Yandex DOM adapter

В `src/sources/yandex/` реализован адаптер favorites (`/users/:name/tracks`,
`/collection/tracks`, `/collection/liked-tracks`), playlists
(`/users/:name/playlists/:id`, `/playlists/:id`) и albums (`/album/:id`).
Только rendered DOM, без source network, storage, cookies или hidden state.
Все selectors находятся в `selectors.ts`. Подробнее — [ADR-0013](../docs/adr/0013-yandex-rendered-dom.md).

Использование в content context (consumer должен сохранять/подтверждать batch
до следующего `next()`, а не собирать всю библиотеку в массив):

```ts
const adapter = createAdapter(new URL(location.href), document);
const controller = new AbortController();
const capture = adapter.collectAllTracks({
  signal: controller.signal,
  maxTracks: 100_000,
  onProgress: progress => updateProgress(progress),
});
for (;;) {
  const result = await capture.next();
  if (result.done) {
    showSummary(result.value); // complete или partial с причиной
    break;
  }
  await acceptBatch(result.value.tracks);
}
// Кнопка отмены вызывает controller.abort().
```

`createAdapter` импортируется из `src/sources/yandex/adapter.ts`; функции
updateProgress/showSummary/acceptBatch здесь обозначают consumer, не готовый RPC.
Адаптер зарегистрирован для page.info и подключён к Import all / Select tracks;
демо запускается отдельной кнопкой.
Progress доступен callback-ом и в overlay; backend DTO не изменён.

Сбор перемещает список к началу и прокручивает с перекрытием. Не переключайте
коллекцию во время прохода: navigation/root/title/count changes завершают partial.
Нет ID — metadata digest только для локального dedup, результат partial. Таймаут
и нижняя граница сами по себе не подтверждают полноту. Hidden/incomplete rows,
loading или несовпавший счётчик не превращаются в успешный полный экспорт.

Fixtures в `tests/fixtures/yandex/` синтетические; tests моделируют layout и
виртуализацию в jsdom, без сетевых запросов. Совместимость selectors с текущей
живой страницей не заявляется до ручной проверки rendered DOM.

## Spotify DOM adapter

`src/sources/spotify/adapter.ts` предоставляет `SpotifyAdapter` и `createAdapter`.
Контракт и usage совпадают с Yandex: `collectAllTracks({signal, maxTracks,
onProgress})` возвращает AsyncGenerator batches и итоговый CaptureSummary.
Поддержаны Liked Songs (`/collection/tracks`), `/playlist/:id`, `/album/:id`
на `https://open.spotify.com`, включая `/intl-xx/` prefix.

Собираются title (с live/remaster qualifiers), artists, optional album/duration,
DOM track link и его ID. Без ID metadata остаётся доступной, но capture partial.
Сбор прокручивает основной список с начала с перекрытием, учитывает повторное
использование DOM nodes и delayed/infinite loading. Карточки рекомендаций,
плеер, queue и скрытые строки исключаются. Никаких Spotify API/network responses,
React/internal state, cookies или source storage.

Полный результат требует подтверждённого видимого конца; неподтверждённая полнота,
лимиты или неполная metadata дают partial. Counter распознаёт точные английские/
русские значения; неизвестная локаль или округлённый count не угадываются.
Selectors и service-specific logic локальны в `sources/spotify/`.

Fixtures `tests/fixtures/spotify/` — синтетические contracts, live DOM текущего
Spotify ещё не проверен. Тесты: `npm test`, `npm run typecheck`.
Spotify подключён к режимам Import all / Select tracks через общий selection engine.
Решения и ограничения: [ADR-0014](../docs/adr/0014-spotify-rendered-dom.md).

## VK Music DOM adapter

`src/sources/vk/adapter.ts` предоставляет VKAdapter/createAdapter с тем же
MusicSourceAdapter: bounded collectAllTracks, onProgress и AbortSignal. Общий
interface/domain не изменены. Selectors и вся VK-specific логика локальны.

Поддержаны сохранённые треки `/audios:owner` и `/music` с видимой выбранной
«Моя музыка»/«Сохранённые треки», playlists `/music/playlist/:owner_:id` и
`/audio_playlist:owner_:id`, albums `/music/album/:owner_:id` с видимым списком.
Playlist, который UI явно помечает «Альбом», тоже распознаётся как album.
Карточка/обложка без track list — unsupported.

UI может открывать playlist в dialog через `?z=audio_playlist...`. Адаптер
выбирает только передний dialog, не фоновые треки. Query-only смена коллекции
останавливает проход. Share/access suffix и query не включаются в metadata URL.
Не читаются data-audio/data-full-id, hidden state, cookies, storage или network.
Отдельные artists берутся из ссылок; plain-text performers не делится по запятым.

Virtualized/infinite traversal идёт с начала списка с перекрытием; dedup хранит
компактные ID/digest, batches ограничены. Неподтверждённый конец/ID-less metadata
дают partial. Fixtures `tests/fixtures/vk/` синтетические; live UI не проверен.
Tests включают 1 200 виртуальных треков при трёх DOM nodes.

VK подключён к Import all / Select tracks, как Spotify/Yandex. Детали: [ADR-0015](../docs/adr/0015-vk-rendered-dom.md).

## Import all / Select tracks

1. Подключитесь к backend, выберите существующую destination collection и profile.
2. Откройте поддержанную страницу Spotify/Yandex/VK и popup расширения.
3. **Import all** прокрутит доступную коллекцию, покажет полноту и автоматически
   запустит upload собранных треков. Неизвестный конец даёт явно отмеченный partial.
4. **Select tracks** покажет отдельный checkbox overlay рядом с видимыми строками.
   Отметки сохраняются при scroll, удалении DOM nodes и закрытии popup.
   «Выбрать все с прокруткой» проходит доступный список; это не только viewport.
   «Очистить выбор» снимает все отметки. Счётчик показывает сохранённый выбор.
5. «Начать импорт» фиксирует выбор и запускает загрузку; checkbox больше не меняет
   frozen snapshot. Backend progress и retry находятся в popup.
6. «Отмена» на странице или «Отменить выбор / сбор» в popup прекращает capture,
   очищает selection; если upload уже начат — ставит durable backend cancel.

Selection хранится в IndexedDB расширения, по source key либо normalized metadata
SHA-256. DOM node не является identity, полная metadata библиотеки не держится в RAM.
Selected subset явно передаётся как partial; разные известные track IDs не сливаются.
Source cookies, storage и tokens не читаются и не передаются. Создание destination
пока не реализовано: если список назначений пуст, запуск недоступен.

Один активный capture. Новый режим заменяет прежний выбор; во время upload замена
запрещена. Navigation/reload требует нового режима, автоматического UI восстановления
после reload нет. Worker restart сохраняет выбор и upload. После upload snapshot
хранится до нового режима/cancel/logout. Выход удаляет local snapshot/job/token;
удалённую задачу при необходимости отмените до выхода.

Selectors проверены на синтетических fixtures. Ручной smoke: установить сборку,
открыть доступную коллекцию, отметить трек, прокрутить до его удаления из DOM и
вернуться; убедиться в сохранении отметки, затем проверить clear/start/cancel.
Live-site и Firefox runtime совместимость не подтверждаются одной сборкой.
