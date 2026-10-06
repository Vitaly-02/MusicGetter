# Spotify rendered DOM fixtures

Синтетическая обезличенная разметка; это не live snapshots текущего Spotify.
HTML scripts не исполняются, resources/network не загружаются. Cookie/storage/
network API и чтение JSON script в fixture запрещены тестовыми tripwires.
jsdom не выполняет layout: helpers явно моделируют viewport/clipping/scroll.

- playlist.html — несколько artists, album, duration, locale link, recommendation.
- album.html — disc/header rows, album fallback, hidden descendants, unknown duration.
- liked.html — ID-less/local metadata, visible unavailable track, hidden row.
- virtualized.html — три переиспользуемые строки, nested viewport; тест моделирует
  десять треков и delayed infinite loading с растущим scrollHeight.

Selectors меняются только в src/sources/spotify/selectors.ts. Исправление регрессии
сопровождается минимальным фрагментом видимой разметки и тестом; не сохраняйте
полную страницу, user identifiers, cookies или hidden payloads.
`npm test` запускает parser/capture suites; `npm run typecheck` проверяет контракты.
