# ADR-0003: PostgreSQL как БД и очередь

Статус: принято. Дата: 2026-10-05.

## Контекст

Импорт должен переживать рестарты и обрабатывать десятки тысяч треков без Redis/RabbitMQ.

## Решение

Captures, batches, items и jobs хранятся в PostgreSQL. Создание jobs атомарно с изменением domain state. Claim — короткая транзакция SELECT FOR UPDATE SKIP LOCKED, далее lease/heartbeat/generation и bounded retry. Внешний I/O вне транзакции.

## Альтернативы

In-memory goroutines не дают durable processing. Redis/RabbitMQ не вводятся по требованию. Удержание SQL row lock во время сетевого вызова отвергнуто.

## Последствия

Обработка at-least-once. Нужны индексы, reaper, backoff, stale-worker fencing и monitoring. Fencing защищает БД, но не отменяет уже отправленный внешний запрос; для него действует ADR-0004.
