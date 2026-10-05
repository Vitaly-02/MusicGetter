# ADR-0007: Явный SQL и проверки важных решений

Статус: принято. Дата: 2026-10-05.

## Контекст

Транзакции, уникальность и конкурентные claims — основа correctness; ORM или mock-only tests могут скрыть ошибки.

## Решение

PostgreSQL versioned SQL migrations, pgx и небольшие repositories по use case. sqlc вводить, когда реальные запросы докажут пользу; generated files не править вручную. Поведенческая матрица в docs/verification.md обязательна при реализации.

## Альтернативы

Универсальный CRUD repository/ORM и premature code generation отвергнуты. Тесты пустых interfaces не заменяют проверок транзакций.

## Последствия

Сейчас только logical schema и declarations, миграций/бизнес-тестов нет. DB constraints и concurrency тестируются на реальном PostgreSQL; compiler checks не подтверждают бизнес-корректность.
