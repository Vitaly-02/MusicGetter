# ADR-0016: Выбор треков, DOM capture и durable selection snapshot

Статус: принято. Дата: 2026-10-07. Расширяет ADR-0012–0015.

## Решение

Popup предоставляет Import all и Select tracks после подключения, выбора source
profile и существующей destination collection. Demo остаётся отдельным opt-in.
Import all проходит доступный DOM с прокруткой и запускает импорт результата;
partial явно отображается, unsupported/смена коллекции не маскируются успехом.
Select tracks открывает checkbox overlay, количество выбранных, «Выбрать все с
прокруткой», очистку, запуск и отмену. Select all означает обход доступного списка,
а не обещание доступа к отсутствующим данным. До finish/freeze выбранные tracks
не отправляются backend. Capture progress отображается на странице, upload — в popup.

MusicSourceAdapter расширен одной optional capability selectionRows():
SelectionRow[] (element + Track). Тонкие sources/{spotify,yandex,vk}/selection.ts
используют только свой DOM parser/visibility/metadata. Core не знает selectors
или сервисных URL. SelectionView — общий fixed overlay в closed ShadowRoot,
без вставки children в track rows и без изменения site handlers/styles.
Обрабатываются MutationObserver, scroll/resize и редкий polling для SPA navigation.
Перед toggle асинхронно перепроверяется identity строки: переработанный DOM node
не получает отметку прежнего трека. При смене коллекции режим блокируется.

## Identity и хранение

Общий SelectionEngine работает через async SelectionPort. Identity v1:
- source + точный source_track_key, если он есть;
- иначе SHA-256 от source, NFKC → lowercase → NFKC → whitespace normalized title,
  artists (unique/sorted), album, exact duration/null и version.
Position, node и source_url не входят в identity. Это отдельный локальный формат,
**не** Go canonical fingerprint v1: Unicode case folding отличается от JS lowercase.
Разные известные source IDs не объединяются; ID-less идентичные descriptions
неразличимы, aliases при появлении ID автоматически не создаются.

Metadata выбранных tracks находится в extension-origin IndexedDB
musicgetter-selection-v1 (stores state/tracks), а не page storage или полном
in-memory Map. Один активный capture, до 100 000 выбранных записей. В content RAM
только текущие rendered rows/batch; adapter traversal хранит compact dedup keys.
Selection DB транзакционно обновляет membership/count, отклоняет late mutations
по capture ID/state и становится immutable при start. Все операции UI
сериализуются; clear не смешивается с незавершённым select-all.

Выбор переживает удаление/recycling DOM nodes, закрытие popup и restart background.
Reload/navigation страницы требует нового явно открытого режима; автоматическое
восстановление UI после reload не реализовано. Новый режим заменяет старый выбор
(во время активного upload замена запрещена). Cancel очищает metadata и закрывает
UI, logout удаляет selection и credential/job. После upload snapshot остаётся до
нового выбора/cancel/logout. Browser quota error не превращается в успешный select.

## RPC и запуск импорта

Popup-команды по-прежнему требуют exact trusted popup sender. Новые content RPC
привязаны к extension ID, tab ID, frame 0, HTTPS source host, document nonce,
capture ID, owner и backend origin. Только popup открывает capture и задаёт
profile/destination. Content не получает credential, owner или API transport.
Strict field allowlists, 200 tracks/512 KiB messages и source-specific URL host
validation действуют до записи. Глобальная сериализация background сообщений
согласует selection/start с pairing/logout. Stored hashes не являются credentials.

Freeze и создание job находятся в разных extension DB: frozen snapshot + job ID
равны capture ID; повтор start после потерянного ACK/worker stop безопасно завершает
создание той же job. Frozen snapshot больше не меняется. SelectionProducer читает
его keyset pages <=200 tracks с byte budget. Pending chunk/key/cursor сохраняются
до POST; cursor продвигается только с ACK. Lost ACK/restart повторяет прежний body.
Порядок keyset identity не обещает исходный playlist order; DTO сохраняет наблюдённую
position, identity от неё не зависит. Backend API/Go schema не меняются.

Выбранная подвыборка создаёт source kind=selection и отдельный collection key
selection:<capture UUID>, чтобы не конфликтовать с original favorites/playlist/album
kind в PostgreSQL. Source profile остаётся пользовательским, canonical track и
membership dedup продолжают действовать между импортами. Selected capture имеет
partial/user_stopped; Import all передаёт complete только при подтверждении adapter
и совпадении durable count. Изменение membership после seal инвалидирует summary.
Отмена начатого upload использует существующий durable cancel/backend API.
Локальный cancel разрешён с истёкшей собственной session того же владельца,
чтобы записать намерение отмены до переподключения; другие операции требуют
действующей session.

## Проверки и границы

Tests: selection normalization, source/key distinctions, concurrent duplicate sets,
freeze/clear/reset races, IndexedDB reopen, count/UTF-8 pagination, RPC sender/owner/
source isolation, lost start ACK, lost HTTP ACK + worker restart, all/selected seal,
cancel и checkbox recycling/removal для всех трёх sources на DOM fixtures.
Исправлена устаревшая проверка page.info, принимавшая только adapter=stub / supported=false.

Fixtures синтетические и layout моделируется jsdom. Live selectors и ручной
браузерный smoke не заявляются проверенными. После upload backend import становится
queued: worker/matcher/destination execution остаётся вне этой задачи.
