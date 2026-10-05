# ADR-0001: DOM-only и изолированные source adapters

Статус: принято. Дата: 2026-10-05.

## Контекст

Данные доступны в пользовательской web session; использование любых API стримингов и credentials запрещено.

## Решение

Собирать только отрендеренные доступные данные в TypeScript MV3 content scripts. Spotify/Yandex/VK имеют независимые adapters; core владеет bounded outbox и HTTPS к MusicGetter. Не использовать hydration stores, private endpoints, XHR/fetch interception или streaming tokens.

## Альтернативы

Официальные API, private clients и network interception отвергнуты прямым ограничением проекта. Универсальный adapter отвергнут из-за разных DOM и независимых изменений сервисов.

## Последствия

DOM хрупок, полнота ограничена реально просмотренными страницами. Partial — нормальное состояние; селекторы покрываются fixtures каждого сервиса. Автоматическое полное извлечение без участия пользователя не обещается.
