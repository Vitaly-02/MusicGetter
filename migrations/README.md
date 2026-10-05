# PostgreSQL migrations

Логическая схема: ../docs/database.md. SQL migrations намеренно ещё нет:
архитектурная задача не создаёт production schema и DB layer.

При реализации: последовательные versioned SQL файлы, например
000001_initial.up.sql и 000001_initial.down.sql, с выбранным migration runner.
Один runner при deployment, не каждый API instance. Транзакция там, где DDL её
поддерживает; CREATE INDEX CONCURRENTLY отдельно. Downgrade с потерей данных
не выполнять автоматически: документировать backup/forward-repair процедуру.
pgx — DB driver; sqlc добавлять только вместе с запросами, config и генерацией.
