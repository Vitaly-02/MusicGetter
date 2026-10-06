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

Spotify и VK — независимые заглушки; Yandex DOM adapter реализован и проверен
на синтетических fixtures, см. раздел ниже. Stub возвращает supported=false,
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
getCollectionMetadata, collectVisibleTracks, collectAllTracks, observe.
collectAllTracks возвращает **AsyncGenerator ограниченных пакетов**, а не
Promise всей библиотеки — для десятков тысяч треков.

Каждый реальный adapter использует только видимый DOM и разрешённую прокрутку.
Подключение Yandex к content → background producer — отдельный этап:
нужны документ/capture ID, подтверждение каждого пакета, backpressure и partial
при потере документа. Текущий content bridge предоставляет только `page.info`;
runtime передачи DOM-треков намеренно не объявлен готовым. Рабочий pipeline сейчас
проверяется через DemoProducer и tests, без обращений к стримингам.

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
Адаптер зарегистрирован для page.info, но **реальный импорт из popup пока не
подключён**: существующая кнопка запускает только явно отмеченное демо.
Progress доступен callback-ом; backend DTO не изменён.

Сбор перемещает список к началу и прокручивает с перекрытием. Не переключайте
коллекцию во время прохода: navigation/root/title/count changes завершают partial.
Нет ID — metadata digest только для локального dedup, результат partial. Таймаут
и нижняя граница сами по себе не подтверждают полноту. Hidden/incomplete rows,
loading или несовпавший счётчик не превращаются в успешный полный экспорт.

Fixtures в `tests/fixtures/yandex/` синтетические; tests моделируют layout и
виртуализацию в jsdom, без сетевых запросов. Совместимость selectors с текущей
живой страницей не заявляется до ручной проверки rendered DOM.
