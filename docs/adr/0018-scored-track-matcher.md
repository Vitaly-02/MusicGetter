# ADR-0018: Scored matcher, staged search и mapping cache

Статус: принято. Дата: 2026-10-07. Уточняет matching ADR-0009/0017.

По запросу пользователя exact-only matcher заменён в worker на scored_metadata_v1.
Canonical fingerprint v1 не меняется: Unicode/пунктуация/ё и version-aware scoring
живут отдельно в matcher. Признаки live/remix/remaster/radio edit/sped up/slowed/
instrumental/acoustic сохраняются в evidence и ограничивают auto-accept.
Алгоритм, веса, пороги, duration bands и ограничения — [matching.md](../matching.md).

## Решение

- Узкий destination search port `SearchTracks(ctx, Query)`; optional TrackSearcher
  рядом с delivery Destination, либо отдельный Catalog в factory Binding.
- Engine получает CanonicalTrack и connection scope, делает до четырёх bounded
  стратегий: artists/title, primary/title, title/album, title-only. Duplicate queries
  пропускаются. Strong match останавливает поиск. Title-only только после пустых
  предыдущих результатов, не после transport error или неподходящих candidates.
- Pure Scorer не знает transport/storage; Engine владеет sequential search,
  accumulated candidates и ограниченным TTL/LRU cache. Scores remote не доверяем.
- Persistent TrackMapping проверяется pipeline **до** Engine. Успешный mapping
  переиспользуется без remote search в том числе для provisional identity и старой
  policy, как явно потребовал пользователь. Это заменяет повторную проверку
  metadata-only mappings ADR-0017/0009. БД уже имеет owner-scoped unique mapping;
  нового DDL нет. Запись остаётся под job lease/fencing в Matched transaction.
- TTL search cache scoped owner+connection+policy+full typed query, bounded entries
  и bytes, короткий negative TTL. Ошибки не кешируются. Same-query coalescing,
  context-aware shared semaphore и timeout; cache/limits per worker process.
- Неоднозначность нельзя снять сортировкой: AUTO_MATCH требует threshold, evidence
  gates и margin от второго кандидата. Truncated pages/conflicting IDs — ambiguous.

## Последствия и альтернативы

Больше recall для Unicode/typos/metadata gaps без потери edition qualifiers.
Без remote requests по cached mapping массовые повторные imports быстрее, но
ошибочное принятое соответствие теперь закрепляется и для metadata-only identity.
Нужна явная инвалидация для исправления, UI/API которой ещё нет; policy upgrade
не меняет прежний выбор молча. Fingerprint остаётся metadata identity, не audio ID.

Eager parallel fallback увеличивал бы remote traffic и мешал early exit; выбран
sequential query plan с общим bounded concurrency между разными items. Глобальный
cache без account scope отвергнут. Persistent search-result table не добавлена:
короткий process TTL достаточен, accepted matches уже durable в PostgreSQL.
Безусловное удаление version markers и aliases dictionary отвергнуты.

Thresholds проверены на synthetic fixtures/table tests, а не калиброваны на live
destination. Реального музыкального adapter по-прежнему нет. Production wiring
остаётся fail-closed при отсутствии зарегистрированного destination.
