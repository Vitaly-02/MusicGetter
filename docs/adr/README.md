# Architecture Decision Records

Все решения приняты для архитектурного каркаса 2026-10-05; открытые параметры
перечислены в architecture.md. Изменения оформлять новым ADR со ссылкой на заменённый.

- [0001 — DOM-only и изолированные source adapters](0001-dom-only-source-adapters.md)
- [0002 — Модульный Go backend и TypeScript extension](0002-modular-go-monorepo.md)
- [0003 — PostgreSQL как БД и очередь](0003-postgres-durable-jobs.md)
- [0004 — Идемпотентность по destination membership](0004-membership-idempotency.md)
- [0005 — Управляющий bot отдельно от музыкального destination](0005-destination-capabilities.md)
- [0006 — Порционный immutable capture и явная полнота](0006-incremental-captures.md)
- [0007 — Явный SQL и проверки важных решений](0007-schema-and-verification.md)
- [0008 — Backend bootstrap, миграции и lifecycle](0008-backend-bootstrap.md)
- [0009 — Domain identity, constraints и persistence](0009-domain-identity-persistence.md)
- [0010 — Telegram bot, pairing и extension sessions](0010-telegram-pairing-sessions.md)
