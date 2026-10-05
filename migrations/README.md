# PostgreSQL migrations

Runner — Goose Provider с pgx stdlib driver; SQL встроен через embed.FS в
`cmd/migrate`. Запуск из любой директории собранным бинарником, без внешних SQL files.
`make migrate-up` применяет ожидающие migration; `make migrate-down` откатывает
ровно одну. DATABASE_URL обязателен; общий deadline задаёт MIGRATION_TIMEOUT.
Backend при запуске не меняет schema. Readiness проверяет expected version и namespace.

Bootstrap `00001_bootstrap.sql` создаёт только пустую schema `musicgetter`;
бизнес-таблицы из docs/database.md не реализованы. Migration history хранится в
`public.goose_db_version`. Down использует DROP SCHEMA RESTRICT, поэтому при
наличии объектов не уничтожит их и оставит migration применённой.

Новые миграции: последовательный `00002_name.sql`, секции `-- +goose Up` и
`-- +goose Down`. Изменять только ещё не применённые файлы; исправления — новой
миграцией. Обновлять Version в embed.go. Goose выполняет каждую SQL migration
в транзакции; session advisory lock сериализует migration runners. Lock wait
ограничен command context. Обычные HTTP replicas не запускают runner.

DDL с CREATE INDEX CONCURRENTLY потребует отдельного явно документированного
нетранзакционного шага; не добавлять его в обычную транзакционную migration.
Для каждого изменения описать совместимость, блокировки, rollback и потерю данных.
Откаты с потерей данных не автоматизировать: backup/forward repair перед действием.
Запускать make test-integration на PostgreSQL. Базовый down безопасен только для
пустой schema; history table Goose после него остаётся для будущего up.
