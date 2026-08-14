# Пошаговый план разработки

План разбит на вертикали, каждая заканчивается демонстрируемым результатом. Технические P0–P2 решения закрыты в [technical-decisions.md](technical-decisions.md). Этап 1 завершён 13 августа 2026 года; этапы 2–3 находятся в работе. Календарные оценки зависят от состава команды и не меняют технический порядок.

## Три крупные итерации

| Итерация | Этапы | Демонстрируемый результат | Статус |
| --- | --- | --- | --- |
| I. Платформенная основа | 1 | Два запускаемых сервиса, инфраструктура, криптография и machine-readable contracts | Завершена |
| II. Надёжное прикладное ядро | 2–5 | Inbox/outbox и полный activation/list/revoke flow без реальной Telegram network | В работе: activation verification доступна для demo/integration |
| III. Транспорты и production readiness | 6–8 | Telegram modes, natsproxy, diagnostics, hardening и production handoff | Начат только local polling demo |

## Этап 0. Архитектурное согласование — завершён

### Работы

1. ADR-001…ADR-009 утверждены.
2. Top-up, natsproxy и diagnostics boundaries зафиксированы как обязательные v1 contracts + reference fakes.
3. Network/address, balance ID, invite/session/revoke policies определены.
4. Go/PostgreSQL/NATS versions и deployment platform зафиксированы.
5. AsyncAPI subject taxonomy, schemas и Ed25519 test-vector format определены; сами machine-readable artifacts создаются на этапе 1.

### Результат

Технических блокирующих вопросов нет; обе стороны могут независимо реализовывать contracts и проверяться общим conformance bundle.

## Этап 1. Основа репозитория — завершён

### Работы

1. Инициализировать Go module и toolchain.
2. Создать два `cmd`, package boundaries и dependency rules.
3. Добавить config validation, structured logging, lifecycle, health/readiness/metrics.
4. Подключить PostgreSQL и NATS clients с TLS/auth/reconnect callbacks.
5. Создать migration runner/jobs, Dockerfiles, Compose, Make/Task targets и CI skeleton.
6. Реализовать envelope codec/verifier и golden signature vectors.
7. Реализовать AES-GCM/HMAC keyring, rotation primitives и machine-readable AsyncAPI/JSON Schemas.

### Критерии приёмки

- `make dev-up` поднимает зависимости и оба пустых сервиса.
- Invalid config/key/schema приводит к fail-fast.
- Graceful shutdown и reconnect видны в тестах/метриках.
- Production image работает non-root.

Фактический состав, способы запуска и выполненные проверки приведены в [отчёте первой итерации](iteration-1-result.md).

## Этап 2. Надёжный messaging substrate — основной срез реализован

### Работы

1. Миграции inbox/outbox для обеих БД.
2. Transaction helpers, publishers, consumers и retry/reconciliation workers.
3. Queue groups, bounded concurrency и backpressure.
4. Повтор сохранённого result для duplicate command.
5. Contract harness и fake peer.

Реализованы общие inbox/outbox primitives, encrypted exact envelopes, queue consumers, bounded publisher, retries, duplicate-result replay и reconciliation top-up verification. Полная crash/failure matrix, retention workers и эксплуатационные метрики остаются до закрытия этапа.

### Критерии приёмки

- Crash matrix из testing strategy проходит.
- Offline consumer после восстановления получает повтор command и возвращает один result.
- Duplicate message не дублирует side effect.

## Этап 3. Вертикальный срез active-sessions — activation verification реализована

### Работы

1. Миграции `balance_identities`, `activation_verifications`, `active_sessions`, `session_events`.
2. Domain policies и repository.
3. `activation.verify` → top-up verification → activated/rejected.
4. Immutable first sender wallet под concurrency.
5. List/revoke с authorization и audit.
6. Component tests с fake top-up.

Реализованы пункты 1–4 и activation component demo с fake top-up. `session.list`, `session.revoke`, полная concurrency/failure matrix и production top-up conformance остаются следующей частью этапа.

### Критерии приёмки

- Первая активация создаёт identity+session атомарно.
- Повторная с тем же wallet создаёт ещё одну session.
- Другой wallet получает deterministic rejection.
- Concurrent/duplicate/late events сохраняют invariants.

## Этап 4. Invite и неактивные сессии бота — основной срез реализован

### Работы

1. Миграции invites/inactive sessions/Telegram dedup/rate-limit buckets.
2. Асинхронный invite create/result.
3. Deep-link и manual invite application flow без реального Telegram adapter.
4. Atomic consumption, TTL/cleanup и rate limits.
5. Telegram-neutral presentation commands/events.

Реализованы signed create/result, encrypted single-use invite, atomic binding, ручной/deep-link ввод и отдельная inactive-session table. Распределённые rate limits и retention worker остаются hardening.

### Критерии приёмки

- Invite создаётся идемпотентно и однократно потребляется.
- Конкурентное использование даёт одного победителя.
- Inactive session не имеет active commands.

## Этап 5. Оркестрация активации ботом — основной flow реализован

### Работы

1. Миграции activation attempts и active projection.
2. Persistent state machine.
3. Reserve/retry-later/payment claim flows с fake top-up.
4. Activation verify interaction с real active-sessions.
5. Projection updates и безопасные user notifications.
6. Reconciliation pending/expired/late operations.

Реализованы persistent state machine, reserve/payment/verify, active projection, expiry reconciliation, reference fake top-up и end-to-end demo. Durable Telegram delivery queue и расширенная crash matrix остаются hardening.

### Критерии приёмки

- Полная активация работает end-to-end без Telegram network.
- Ни один top-up claim сам по себе не активирует session.
- Restart на каждом переходе не ломает flow.

## Этап 6. Прямые Telegram-транспорты — polling activation flow реализован

### Работы

1. Bot API client с rate-limit/retry policy.
2. Long polling adapter для local/dev.
3. Webhook adapter, secret validation и deployment config.
4. Команды, inline keyboard/callback token, user-safe error messages.
5. Список/revoke UX и защита active-only functionality.
6. Durable `telegram_deliveries`, unsafe outcome handling и русский presentation catalog.

Срез `direct_polling` доступен для ручной демонстрации: `getMe`, безопасное отключение webhook, long polling, durable update dedup, invite и полный activation flow. Webhook, callbacks, list/revoke UX и delivery queue остаются следующими частями этапа.

### Критерии приёмки

- Основной E2E проходит в polling и webhook modes.
- Повтор update/callback безопасен.
- Telegram outage/429 не теряет domain state и не создаёт message storm.

## Этап 7. natsproxy и diagnostics

### Работы

1. Реализовать adapter по утверждённому natsproxy v1 contract и reference fixtures.
2. Проверить behavioral parity с direct transport.
3. Реализовать diagnostics publisher, aggregation и fallback metrics/logging.
4. Negative tests ACL/signatures.

### Критерии приёмки

- Переключение transport mode не меняет application/domain tests.
- Ошибка diagnostics не блокирует основной flow.
- Контракты проходят в CI обеих команд.

## Этап 8. Подготовка к production

### Работы

1. Полная failure/chaos matrix.
2. Load tests и sizing DB/NATS/worker pools.
3. Security review и secret/PII audit.
4. Backup/restore и projection rebuild rehearsal.
5. Dashboards, alerts, runbooks, deployment/rollback docs.
6. Staging soak и canary rollout plan.

### Критерии приёмки

- Все пункты [review checklist](review-checklist.md) закрыты.
- SLO/alerts согласованы с эксплуатацией.
- Другой разработчик выполняет local quickstart без устных инструкций.
- Техдир подтверждает what-if review.

## Порядок pull requests

Рекомендуется небольшая цепочка:

1. ADR/contracts/docs only.
2. Repository foundation.
3. Signed envelope + fixtures.
4. Inbox/outbox substrate.
5. Active-sessions schema/domain.
6. Active-sessions NATS flows.
7. Bot invite/inactive domain.
8. Activation orchestration.
9. Telegram polling.
10. Telegram webhook.
11. Session management UX.
12. natsproxy/diagnostics.
13. Hardening/operations.

Каждый PR обновляет соответствующую wiki-страницу, миграции/contract schemas и tests.

## Критерии готовности релиза

- Все REQ из scope имеют test traceability.
- Нет реализации OUT-OF-SCOPE функций.
- Публичные schemas versioned и опубликованы вместе с fixtures.
- Миграции протестированы с предыдущей версией.
- Local Compose и production images воспроизводимы.
- Unit/integration/contract/E2E/failure tests зелёные.
- Нет известных P0/P1 security/reliability defects.
- Runbooks, dashboards, alerts, backup/restore и rollback проверены.
- Логи/метрики/traces не содержат secrets и high-cardinality IDs.
