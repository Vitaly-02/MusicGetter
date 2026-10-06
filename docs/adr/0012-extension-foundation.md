# ADR-0012: Manifest V3 foundation, private credentials и durable outbox

Статус: принято. Дата: 2026-10-06. Уточняет ADR-0002/0006/0010/0011.

## Решение

Chrome/Chromium first, TypeScript + esbuild без runtime dependencies. Chrome
service worker и Firefox event background собираются разными manifest из одного
исходного кода. Browser APIs изолированы узким портом, core не зависит от DOM
конкретного сервиса. Source adapters изолированы, пока все три — явные stubs.
API DTO соответствует docs/openapi.json. MusicSourceAdapter.collectAllTracks
возвращает AsyncGenerator ограниченных пакетов вместо Promise<Track[]>.

Content script инжектируется только после пользовательского открытия popup,
через activeTab/scripting в распознанный source host. Нет постоянного доступа
ко всем вкладкам, cookies, webRequest, debugger, remote code и page bridge.
Текущий content RPC — только page.info. Управляющие сообщения принимает только
точный runtime sender popup.html своего extension ID, без sender.tab.

Backend origin фиксируется при build; optional host permission спрашивается в
user gesture. HTTPS либо HTTP loopback для dev. CSP connect-src и клиент привязаны
к точному origin; redirects запрещены, credentials:omit. Стриминговый host нельзя
указать в качестве backend. Любые будущие DOM observations проходят strict DTO
validation; HTML/extra fields, source credentials и query-bearing links запрещены.

Token хранится в extension-origin IndexedDB, доступной только trusted extension
contexts; popup не получает token через RPC. В отличие от chrome.storage.local,
web storage content script относится к origin страницы. Никаких ключей «шифрования»,
хранящихся рядом с токеном, и обещаний защиты от владельца ОС. Logout атомарно
удаляет local credential + job и отменяет локальный fetch. Remote revoke всех
sessions остаётся в боте; remote import cancel — отдельное явное действие.

Одна durable job и один pending chunk до 512 KiB. Ключ/sequence/body сохраняются
до отправки; ACK проверяется перед удалением. Job привязана к owner и backend
origin. Read-modify-write IndexedDB транзакционный, late response после logout
не воскрешает job; ошибка старого token не удаляет новый token. Worker wakeups
сериализованы, обработка ограничена четырьмя шагами на wake. Alarms раз в минуту
и popup events продолжают работу без artificial keepalive. Delay/attempts durable:
exponential backoff с jitter + Retry-After, 8 попыток, затем manual resume.

Cancel приоритетен и остаётся pending при offline. Потерянный create ACK требует
сначала восстановления ID тем же request key. После cancel не отправляются новые
chunks. Одноразовый pairing claim не повторяется автоматически: потеря ответа
требует нового /connect. Неподтверждённый /me после claim тоже требует нового кода.

## Границы реализации

Рабочий ingestion pipeline подключён к явному opt-in DemoProducer (450 synthetic
tracks), source=spotify/profile=demo:*. Это не чтение библиотеки. Реальный DOM
producer/сбор после navigation/scroll будут следующими задачами. Не создавать
пустой «успешный» capture из stub. Новая destination автоматически не создаётся.

Firefox manifest собирается, но ручная совместимость браузера не подтверждена.
Подготовка к AMO/Chrome Web Store, signing и disclosure forms — отдельный этап.
Доступ к профилю ОС и компрометация trusted extension code вне модели защиты.

## Проверки и источники

Unit tests: bounds, API wire options, 429, no retry pairing, ACK loss/restart,
concurrent wakeups, cancel, logout race, owner isolation, credential projection,
IndexedDB transactions через fake-indexeddb. Typecheck + две browser builds.

- [Chrome: storage origins и content scripts](https://developer.chrome.com/docs/extensions/develop/concepts/storage-and-cookies)
- [MDN: background service worker/scripts](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/manifest.json/background)
