# VK Music rendered DOM fixtures

Синтетические обезличенные contracts, не live snapshots VK. JS/resources отключены;
jsdom viewport, clipping и scroll моделируются tests/vk-dom.ts. Network/cookies/
source storage, data-audio и data-full-id чтение запрещены тестовыми tripwires.
JSON script и внутренний атрибут в saved.html — запрещённые приманки.

- saved.html — legacy audio_row, несколько artists, plain-text performers,
  version, album, duration, hidden row и recommendations.
- playlist.html — dialog поверх фоновой библиотеки: выбирается только dialog.
- album.html — отдельная modern разметка, explicit album kind, track без ID.
- virtualized.html — три переиспользуемые строки; tests моделируют рост списка
  и проход 1 200 треков без хранения всей DOM библиотеки.

Меняйте selectors только в src/sources/vk/selectors.ts; регрессию фиксируйте
минимальным обезличенным rendered fragment и поведенческим тестом. Полную страницу,
cookies, user credentials или скрытые payloads в fixtures не добавлять.
Проверка из extension: npm test и npm run typecheck.
