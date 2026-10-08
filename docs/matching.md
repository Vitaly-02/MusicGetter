# Track matching

Рабочая policy: `scored_metadata_v1`. Она сопоставляет metadata, не аудио.
Код: `internal/matcher`; wiring — `cmd/worker`, persistent checkpoints —
`internal/storage/postgres/pipeline_repository.go`. Реального музыкального destination
пока нет: тесты используют contract fake, не Spotify/VK/Yandex API.

## Контракты и поток

`Engine.Match(ctx, CanonicalTrack, connectionID, Catalog)` возвращает Decision:
matched / ambiguous / not_found, selected track только для matched, ranked candidates
со score, безопасными reason codes и PolicyVersion. `Catalog.SearchTracks(ctx, Query)`
— узкий search port destination. Destination может реализовать optional
`destination.TrackSearcher`; либо factory передаёт отдельный Catalog в Binding.
Pipeline умеет оба варианта, domain не зависит от протокола музыкального бота.

Сначала pipeline читает PostgreSQL TrackMapping по owner/canonical track/connection.
Сохранённый успешный match (manual или automatic) используется **без remote search**,
включая track без source key и policy предыдущей версии. Это намеренно меняет
повторную проверку provisional mappings из ADR-0017. Canonical metadata immutable;
совпадение fingerprint не доказывает акустическую идентичность. Изменение metadata
без source key создаёт другую canonical identity и новый поиск.

Новый successful match сохраняется транзакционно с checkpoint item=matched под
lease/fencing. Неоднозначные результаты и misses не создают TrackMapping.
`UNIQUE(canonical_track_id,connection_id)` и `ON CONFLICT DO NOTHING` сохраняют
прежний выбор, конфликт другого destination ID не перезаписывается. Ошибка доставки
не стирает match. Перезапуск worker не сбрасывает mapping. Не вводится новая таблица
или migration: persistent cache уже был частью schema 7.

У mappings нет TTL. Исправление ошибочного/устаревшего соответствия требует явной
инвалидации/ручного решения; публичный UI/API инвалидации пока не реализован.
Policy upgrades сами по себе не удаляют принятые решения или membership ledger.

## Нормализация только для matching

Canonical fingerprint v1 **не меняется**. Отдельная matching normalization:

- Unicode NFKC + Unicode case folding (включает lowercase), trim/схлопывание пробелов.
- `ё` и `е` эквивалентны. Диакритика и алфавит сохраняются, транслитерации нет.
- Пунктуация/разделители превращаются в пробелы; прямые и типографские апострофы
  удаляются (`Don't` = `Dont`). Разные исходные canonical identities не объединяются.
- Artist arrays — источник имён. Нет словаря aliases, выдуманных псевдонимов,
  автоматического разбиения названий групп по `&`, `/` или запятым.
- `feat`, `ft`, `featuring` извлекаются из title/artist в дополнительное имя артиста.
  Нормализуется регистр/точка/скобки. Несколько имён в одной feature-строке не
  угадываются: неструктурированное `A & B` не обязано совпасть с массивом `[A,B]`.
- Edition annotations распознаются в скобках, после разделителя ` - ` / ` — ` / `: `,
  в поле version и для известных suffix форм. `live` как обычное слово в начале
  `Live Forever` не удаляется. `acoustic`, `remix`, `remaster`, `radio edit`,
  `sped up`, `slowed`, `instrumental` в suffix трактуются как признаки версии.

Маркеры выносятся из основного title, **но не выбрасываются**: категории и descriptor
(год remaster, имя remix, место live и прочие слова) остаются отдельными evidence.
`remastered` = `remaster`, `slowed down` = `slowed`; служебное `version` у распознанной
категории опускается. Порядок descriptor tokens незначим. Неизвестное непустое поле
version сохраняется как категория other. В каждом поисковом запросе descriptor
сохраняется; original/studio не подменяет запрошенный live/remix.

## Query strategies и budget

Запросы последовательны, до 50 candidates на ответ и до 200 за один Match:

1. Все нормализованные artists + title + version descriptor.
2. Primary artist (первый исходный artist до feature) + title + descriptor.
3. Title + descriptor + album, если album известен и auto-match ещё не найден.
4. Title + descriptor без artist — **только если предыдущие ответы пусты**.

Одинаковые query texts дедуплицируются: для одного артиста первая и вторая стратегии
обычно совпадают. При сильном уникальном match дальнейших запросов нет. Если пришли
неподходящие кандидаты, это не разрешение на title-only fallback. Transport errors,
invalid pages и timeout не трактуются как пустой результат и не запускают fallback:
повтором управляет durable worker с backoff.

Candidates накапливаются по destination ExternalKey между стратегиями. Один ID
с конфликтующей metadata или NextCursor означает неполные evidence и запрещает
AUTO_MATCH. Pagination автоматически не выкачивается: результат ambiguous даже
при сильном кандидате. Два разных destination IDs с одинаковыми metadata сохраняются
разными кандидатами. Remote scores/reasons игнорируются; owner/connection/local ID
из ответа не принимаются как идентичность пользователя.

## Scoring

Все similarity значения в `[0,1]`. Основной title: `1 - distance/max(rune lengths)`;
distance — restricted Damerau-Levenshtein (вставка, удаление, замена, соседняя
транспозиция). Для строк длиннее 256 runes используется линейный multiset bigram Dice,
чтобы длинная валидная metadata не вызывала квадратичную работу. Token similarity —
Dice по множествам слов. Artist name similarity — максимум character/token similarity;
artist set score — минимум средней best-match coverage в обе стороны. Это штрафует
недостающих/лишних feature artists. Artist aliases не добавляются.

| Сигнал | Вес |
|---|---:|
| Основной title similarity | 0.40 |
| Artist set similarity | 0.30 |
| Title token similarity | 0.08 |
| Duration | 0.12 |
| Album similarity | 0.05 |
| Exact version categories + descriptors | 0.05 |

Album similarity использует максимум character/token similarity. Если album или
duration отсутствуют хотя бы на одной стороне, их вес исключается и оставшиеся
веса перенормируются. Missing не считается ни точным совпадением, ни нулевой duration.
Exact normalized title + artist set + version дают бонус 0.02, итог не выше 1.
Remote score не участвует в расчёте.

| Абсолютная разница duration | Duration score | Дополнительное ограничение |
|---|---:|---|
| 0–2000 ms | 1.00 | Сильный сигнал |
| 2001–5000 ms | 0.70 | Более слабый сигнал |
| 5001–10000 ms | 0.25 | Auto-match запрещён, итог ≤0.89 |
| >10000 ms | 0.00 | Итог ≤0.69, not_found для такого кандидата |

При title или artist similarity <0.55 итог ≤0.50. Разные version markers или
разные descriptor имеют жёсткий cap 0.69 и не допускают AUTO_MATCH. Исключение:
различие исключительно remaster/original/год remaster — cap 0.85, то есть review,
а не молчаливое принятие. Разные live venues/remix names также не принимаются.

### Thresholds

- **AUTO_MATCH**: score ≥0.92, title ≥0.90, artists ≥0.90, полностью совпадающие
  markers/descriptors; дополнительно exact normalized title+artists **или** известная
  duration с разницей ≤5s. Отрыв от второго кандидата ≥0.06. Нет truncation/conflicts.
- **AMBIGUOUS**: лучший score ≥0.70, но хотя бы одно условие AUTO_MATCH не выполнено;
  также неполные/конфликтующие remote results независимо от score.
- **NOT_FOUND**: нет кандидатов либо лучший score <0.70 при полных results.

Outcome API/domain называется matched (не отдельный persisted status auto_match).
Rank ties сортируются по ExternalKey, но tie никогда не разрешается этим порядком
в автоматический выбор. Missing duration допускает auto только при exact
normalized title/artists и совпадающей версии. Опечатка без duration идёт на review.

## Кэш поиска и concurrency

Отдельный process-local TTL/LRU cache содержит только sanitized search pages.
Ключ: SHA-256 от owner ID, connection ID, policy и **всего Query** (text, strategy,
original metadata context, limit/cursor). Поэтому разные account scopes и различные
context metadata не делят результаты случайно. Положительный TTL по умолчанию 5m,
отрицательный 30s; transport errors, timeout, invalid pages и panic не кэшируются.

Лимиты: 512 entries и 16 MiB консервативного accounted size (JSON ×3 + object overhead),
ответ ≤1 MiB, ≤50 tracks и cursor ≤1024 bytes. LRU вытесняет entries при любом лимите.
Copies на границах исключают мутацию общего cache через artists/duration pointers.
Одинаковые in-flight queries объединяются. Отмена ожидающего caller не отменяет
request другого; при отмене владельца активный waiter может повторить запрос.

Один Engine в cmd/worker имеет общий semaphore, default **4 remote searches/process**,
remote timeout 10s. Очередь ожидания подчиняется caller context, активные flights
ограничены semaphore. Даже panic адаптера освобождает slot и ждующих callers.
Адаптер обязан соблюдать context. Лимиты/cache не общие между replicas; число worker
replicas учитывается в fleet budget. Persistent mappings общие через PostgreSQL,
search cache после restart пуст. Никакого Redis/RabbitMQ или source network доступа.

Настройки: `MATCHER_SEARCH_CONCURRENCY`, `MATCHER_SEARCH_TIMEOUT`,
`MATCHER_CACHE_TTL`, `MATCHER_NEGATIVE_TTL`, `MATCHER_CACHE_ENTRIES`,
`MATCHER_CACHE_BYTES`; defaults в `.env.example`.

## Проверки и ограничения

Table-driven tests покрывают ordinary/feat/русские названия, Unicode/ё, пунктуацию,
опечатки и транспозиции, artist distinctions, все перечисленные edition markers,
годы remaster/remix names/live venues, отсутствующие duration/album, duration bands,
ties/margins, duplicate IDs и conflicting metadata. Cache tests: TTL, negative cache,
owner/connection isolation, LRU/byte budget, coalescing, cancellation, remote timeout,
error/panic cleanup. PostgreSQL: mapping без source ID переиспользуется после создания
нового Engine даже при недоступном поиске; ledger и повторный импорт остаются безопасны.

`go test ./internal/matcher -run '^$' -bench BenchmarkScorer50Candidates -benchmem`
измеряет CPU matching на синтетических 50 candidates. Это не remote latency benchmark.
Thresholds — тестируемая эвристика, пока не калиброванная на размеченном реальном
каталоге. Метаданные не отличают две записи с одинаковым описанием; fuzzy matching
может ошибаться. Review UI и реальный destination adapter остаются отдельной работой.
