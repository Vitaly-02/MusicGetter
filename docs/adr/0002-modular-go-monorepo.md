# ADR-0002: Модульный Go backend и TypeScript extension

Статус: принято. Дата: 2026-10-05.

## Контекст

Нужны единые domain rules, минимальные зависимости и возможность отдельно масштабировать фоновые задачи.

## Решение

Один Go module, процессы server/bot/worker, net/http, slog, PostgreSQL через pgx; TypeScript MV3 extension в том же monorepo. Domain не зависит от инфраструктуры. Конкретные порты небольшие; реализации подключаются в cmd.

## Альтернативы

Микросервисы и внутренний RPC пока не нужны. chi допустим позднее при заметной пользе; сейчас выбран net/http. Отдельные Go modules усложнят согласование контрактов без ясной выгоды.

## Последствия

Общий релиз и schema compatibility; worker deployment возможен независимо от числа API instances. go.mod пока локальный, production toolchain и dependency versions фиксируются при реализации.
