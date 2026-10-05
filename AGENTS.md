# Правила MusicGetter

## Область действия и стадия
Эти правила применяются ко всему monorepo. Перед работой прочитайте
`docs/architecture.md`, `docs/database.md`, `docs/verification.md` и ADR в
`docs/adr/`. Реализован bootstrap HTTP backend и отдельная команда SQL migrations.
Бизнес-логика импорта, worker/bot runtime и реальные source/destination adapters
ещё не реализованы.
Не выдавайте проектируемые возможности за работающие. Пользователь определяет
границы очередной задачи; изменение принятого решения отражайте в ADR.

## Непересекаемые границы
- Backend, worker и управляющий Telegram bot — Go, один Go module.
- Browser extension — TypeScript, Manifest V3.
- Spotify, Yandex Music, VK Music: только чтение данных, уже отрендеренных
  и доступных пользователю в странице. Не обращаться к официальным,
  unofficial/private API, не исследовать внутренние HTTP endpoint'ы.
- Не перехватывать, повторять или анализировать XHR/fetch для извлечения
  музыкальной библиотеки. Не читать hydration stores, внутреннее состояние
  приложения, скрытые JSON payloads, cookies, localStorage/sessionStorage
  или токены стриминга. Не обходить ограничения доступа.
- Допускается пользовательское раскрытие разделов и прокрутка; не обещать
  экспорт не загруженной страницы. Частичный сбор обозначать явно.
- На backend отправлять только разрешённые поля DTO. Не отправлять cookies,
  auth headers, credentials стримингов, HTML, скриншоты или произвольные URL.
- Собственная авторизация MusicGetter отделена от авторизации стримингов.
- Source adapters полностью изолированы: не импортируют друг друга, общие
  типы и DOM utilities находятся в extension/src/core.
- Конкретный музыкальный бот скрыт за Destination. Управляющий бот проекта
  и музыкальный destination — разные роли. Не предполагать, что произвольные
  боты умеют взаимодействовать друг с другом.

## Архитектура и данные
- Domain не зависит от HTTP, Telegram SDK, pgx и адаптеров. Не создавать giant
  interfaces/files; интерфейсы определять по роли, обычно рядом с потребителем.
- Использовать PostgreSQL, pgx, SQL migrations; sqlc вводить при наличии
  повторяющихся запросов и реальной пользы. HTTP — net/http, логи — slog.
- Минимум зависимостей. Redis/RabbitMQ пока не добавлять. Очередь — PostgreSQL,
  jobs + SELECT FOR UPDATE SKIP LOCKED, lease, fencing и ограниченные повторы.
- Идемпотентность обязательна на уровнях приёма batch, jobs и membership.
  Повторный импорт не добавляет существующий destination track в тот же target.
- Не объявлять exactly-once внешнего эффекта без доказуемого механизма.
  Неопределённый результат отправки требует reconciliation, не слепого retry.
- Тысячи и десятки тысяч треков: bounded batches, backpressure, keyset pagination,
  лимиты параллелизма; не загружать библиотеку целиком в RAM или одно сообщение.
- Все чтения/изменения привязаны к владельцу. ID пользователя из входного DTO
  не является авторизацией. Не писать credentials и названия коллекций в логи
  по умолчанию. Secrets только из конфигурации окружения/secret storage.
- Изменение схемы — миграция с описанием совместимости, блокировок и отката;
  тесты на реальном PostgreSQL для UNIQUE, транзакций и конкурентного claim.

## Проверка изменений
- При реализации важных решений добавлять поведенческие тесты, включая сбои
  и concurrency; матрица в docs/verification.md. Не подменять их тестами заглушек.
- Go: make test, make lint; изменения pool/migrations дополнительно проверять
  make test-integration с TEST_DATABASE_URL локального PostgreSQL.
  TypeScript: npm ci && npm run typecheck
  в extension после появления lockfile/toolchain. Проверять DOM adapters на
  обезличенных rendered-DOM fixtures без сетевых запросов к стримингам.
- Не добавлять основную логику, реальные credentials или рабочую интеграцию
  destination в рамках задачи только на архитектуру.
