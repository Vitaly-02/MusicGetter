# PostgreSQL migrations

Runner — Goose Provider с pgx stdlib driver; SQL встроен через embed.FS в
`cmd/migrate`. Запуск из любой директории собранным бинарником, без внешних SQL files.
`make migrate-up` применяет ожидающие migration; `make migrate-down` откатывает
ровно одну. DATABASE_URL обязателен; общий deadline задаёт MIGRATION_TIMEOUT.
Backend при запуске не меняет schema. Readiness проверяет expected version и namespace.

Bootstrap `00001_bootstrap.sql` создаёт только пустую schema `musicgetter`;
00002 создаёт accounts/collections, 00003 — tracks/mappings/memberships,
00004 — imports/items/jobs, 00005 — extension sessions и Telegram dedup.
00006 — chunked import upload/receipt tables и collecting state.
Текущая expected version — 6. Migration history хранится в
`public.goose_db_version`. Down первой migration использует DROP SCHEMA RESTRICT, поэтому при
наличии объектов не уничтожит их и оставит migration применённой.

Новые миграции: последовательный `00009_name.sql`, секции `-- +goose Up` и
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

Миграции 00002–00004 добавляют новые таблицы с UNIQUE и составными tenant FK.
Их Down удаляет domain data; выполнять только после backup/остановки writers либо
в изолированной тестовой БД. Предпочитать forward repair на рабочей базе. Отмена
00004 не удаляет persistent memberships, но отмена 00003 удаляет ledger и может
сделать последующий удалённый retry небезопасным без reconciliation.

00005 изменяет extension_pairings (краткий ACCESS EXCLUSIVE), создаёт sessions/dedup
и обычный индекс imports (блокирует writes при построении). Нужен coordinated
upgrade. Down удаляет sessions/dedup, bot-issued и revoked codes; остальные legacy
challenge codes сохраняются. Совместимость и ограничения — ADR-0010.

00006 берёт ACCESS EXCLUSIVE для замены imports state CHECK, затем создаёт новые
upload/receipt tables. Down теряет receipt history и переводит collecting в cancelled;
прежние uploads нельзя возобновлять. Только coordinated rollback после backup.

00008 добавляет durable destination target bindings; down запрещён при непустом
creation ledger. Подробности блокировок и rollback — docs/database.md.
