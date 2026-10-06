# Rendered DOM fixtures Яндекс Музыки

Это синтетические обезличенные HTML contracts, **не** снимки актуального live DOM.
Никаких cookies, токенов, hydration JSON или скачивания страницы тестами.
JSON в playlist fixture — специально запрещённая приманка; его чтение бросает
ошибку. jsdom запускается без scripts/resources, layout моделируется в тесте.

- playlist.html — CSS modules, несколько artists, album/link/version/duration.
- album.html — legacy d-track, album fallback, hidden descendants.
- favorites.html — нет track ID, missing metadata, разные версии.
- virtualized.html — три переиспользуемые строки для десяти треков, nested scroll.

Selectors меняйте в src/sources/yandex/selectors.ts. При регрессии добавляйте
минимальный обезличенный фрагмент **видимой** разметки и поведенческий тест в
 tests/yandex.test.ts; не сохраняйте страницу целиком или скрытые payloads.
Проверка: `npm test`, `npm run typecheck` из extension.
