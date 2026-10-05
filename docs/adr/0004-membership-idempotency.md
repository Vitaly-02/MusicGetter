# ADR-0004: Идемпотентность по destination membership

Статус: принято. Дата: 2026-10-05.

## Контекст

Batch replay и повторный import не должны создавать дубликаты; сетевой timeout не сообщает, был ли внешний effect.

## Решение

Immutable batches по capture/sequence/digest; source identity отделена от destination identity. Ledger и UNIQUE на connection/target/destination_track_id. Intent и стабильный operation key сохраняются до send. Unknown/pending идут в reconciliation. Native atomic ensure предпочтителен; неподтверждённая безопасность блокирует destination, не допускает слепой retry.

## Альтернативы

Дедуп по title/artist неверно объединяет версии. Один processed boolean не переживает crash после send. Transaction PostgreSQL не даёт exactly-once внешнему боту. Native request key без проверки существующих треков не решает preexisting membership.

## Последствия

В target действуют set semantics, повторные позиции теряются. Read-before-add требует сериализации наших writes и доказанного отсутствия гонки с внешними writers; иначе строгая гарантия невозможна. Unknown может потребовать ручного решения. Ledger живёт дольше истории импортов.
