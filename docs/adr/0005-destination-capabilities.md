# ADR-0005: Управляющий bot отдельно от музыкального destination

Статус: принято. Дата: 2026-10-05.

## Контекст

Конкретный музыкальный бот не выбран, а доступность bot-to-bot integration не подтверждена.

## Решение

Управляющий Telegram bot отвечает за linking, настройки, прогресс, review и коллекции. Destination скрывает разрешённый протокол записи; Catalog скрывает его поиск. Capabilities описывают kinds, atomic ensure, idempotency, membership read и reconciliation. Не заявлять поддержку до contract tests реального adapter.

## Альтернативы

Вызовы конкретного бота внутри pipeline привязали бы domain к протоколу. Предположение о свободном общении любых ботов и неоговорённый user-account client отвергнуты.

## Последствия

Выбор destination — блокер рабочей интеграции, но не архитектуры. Albums могут отсутствовать: explicit playlist mapping или отказ. Создание target тоже требует ledger; rename/delete/reorder отложены.
