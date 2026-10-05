# Roadmap

## 1. Architecture & repository bootstrap

Создать monorepo, зафиксировать архитектурные границы, domain interfaces и правила разработки в `AGENTS.md`. Подготовить документацию основных архитектурных решений.

## 2. Backend foundation

Поднять Go HTTP server, PostgreSQL, migrations, configuration, logging, graceful shutdown и локальное Docker-окружение.

## 3. Domain model & persistence

Реализовать модели пользователей, треков, коллекций, импортов, mappings и memberships. На уровне БД заложить constraints, необходимые для идемпотентности.

## 4. Telegram bot

Создать Telegram-бота для управления аккаунтом, подключения расширения, просмотра импортов, статусов, плейлистов и настроек.

## 5. Browser extension authentication

Реализовать безопасную привязку расширения к Telegram-пользователю через одноразовый pairing code и scoped bearer token.

## 6. Import API

Создать chunked API для передачи больших коллекций из расширения в backend с поддержкой retries и idempotency keys.

## 7. Browser extension core

Создать Manifest V3 extension с общим source-adapter API, popup, progress UI, cancellation и передачей данных в backend.

## 8. Yandex Music adapter

Добавить импорт favorites, playlists и albums из отрендеренного DOM Яндекс Музыки с поддержкой virtualized lists.

## 9. Spotify adapter

Добавить аналогичную поддержку Liked Songs, playlists и albums для Spotify Web без использования Spotify API.

## 10. VK Music adapter

Добавить импорт сохранённой музыки, playlists и albums из web-интерфейса VK.

## 11. Track selection

Добавить режим импорта всей коллекции или вручную выбранных композиций, включая корректную работу выбора при виртуализации DOM.

## 12. Import pipeline

Реализовать очередь импортов, background workers, resume после рестартов, retries и детальный статус каждого трека.

## 13. Idempotency

Гарантировать, что повторный импорт не создаёт дубликаты в favorites, playlists или albums даже при повторных запросах, сбоях и параллельных imports.

## 14. Search & matching engine

Реализовать нормализацию metadata, многоступенчатый поиск, scoring кандидатов, persistent mappings и кэш для уменьшения количества удалённых поисковых запросов.

## 15. Destination abstraction

Создать универсальный интерфейс музыкального destination и fake implementation, чтобы весь pipeline можно было тестировать без реального Telegram music bot.

## 16. Telegram music destination

Реализовать отдельный adapter выбранного музыкального Telegram-бота, включая search, playlists, favorites, rate limits и особенности его протокола.

## 17. Playlist & album workflows

Добавить выбор существующего плейлиста, создание нового, перенос исходных playlist names и fallback импорта albums в playlists при необходимости.

## 18. Observability & reliability

Добавить Prometheus metrics, structured logs, job recovery, cleanup, retry policies и диагностику проблем массовых импортов.

## 19. End-to-end tests

Покрыть сценарии Spotify/Yandex/VK → extension → backend → matcher → fake/real destination, включая повторный импорт и библиотеки в тысячи треков.

## 20. Production hardening

Провести security/performance audit, проверить импорт 10 000+ треков, ограничение параллелизма, восстановление после рестарта и подготовить deployment documentation.