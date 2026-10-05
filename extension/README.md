# Browser extension (проект)

TypeScript contracts находятся в src/core/contracts.ts. Каждый каталог sources
зарезервирован под независимый adapter и его обезличенные rendered-DOM fixtures.
Сборки, package.json и устанавливаемого Manifest V3 пока нет; tsconfig описывает
ожидаемую строгую проверку контрактов.

Будущие компоненты:
- popup/options: привязка, source profile, запуск, состояние и явное завершение;
- content script: изолированный DOM adapter, MutationObserver и ограниченный буфер;
- MV3 service worker: собственная авторизация, очередь и HTTPS к нашему backend;
- IndexedDB: неподтверждённые batches, sequence, capture ID; ограниченная квота.

После ACK пакет удаляется из outbox; до ACK хранится неизменным и повторяется с
тем же sequence. Перезапуск service worker восстанавливает outbox, а не создаёт
новый capture. DOM cursor после перезагрузки страницы не считается надёжным:
сбор может потребовать прохода с начала и дедупликации или нового partial capture.

Manifest реализовать вместе с runtime: MV3, только storage и минимальные
optional host permissions конкретных поддержанных страниц + HTTPS backend.
Не запрашивать cookies, webRequest, debugger, <all_urls>. Не добавлять удалённый
исполняемый код. Сообщения content script валидировать по sender tab/origin,
схеме и размеру. Credentials MusicGetter не передавать content script или странице.
