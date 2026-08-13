# Стратегия тестирования

## Пирамида

| Уровень | Что проверяет | Инфраструктура |
| --- | --- | --- |
| Unit | Domain policies, transitions, validation, signature input | Нет внешних процессов |
| Property/fuzz | Envelope parser, invite token, state transitions, malformed Telegram/NATS input | Go fuzzing |
| Repository integration | Constraints, concurrency, migrations, `SKIP LOCKED` | Реальная PostgreSQL |
| Messaging integration | Subjects, queue groups, reconnect, duplicate/retry | Реальный NATS без JetStream |
| Контрактный | JSON Schema, fixtures, Ed25519 vectors, обратная совместимость | Схемы + стенд отправителя/получателя |
| Component | Сервис с реальной DB/NATS и fake external adapters | Docker/Testcontainers |
| E2E | Полные Telegram user journeys | Telegram stub + оба сервиса + fakes |
| Failure/chaos | Restarts, outages, delay, reordering, duplicates | Controllable proxies/fakes |
| Load | Throughput, lock contention, pool/queue sizing | Production-like environment |

## Обязательные domain cases

- Invite: valid, expired, reused, concurrent consumption, same operation retry.
- Binding: deep link, manual entry, Telegram update duplicate, already bound session.
- Activation: success, retry-later, timeout, partial/late/duplicate claim, external rejection.
- Repeat activation: same wallet succeeds, different wallet rejects, concurrent first activations.
- Sessions: list authorization, pagination, target missing/already revoked, self/last-session policy.
- Projection: duplicate, stale and out-of-order authoritative events.
- Binding lifecycle: одна active/non-terminal binding на Telegram identity, новая binding после terminal revoke/expiry.
- Rate limits: distributed atomic counters, retry-after, cleanup and no raw identifiers in bucket keys.

## Сценарии надёжности

Для каждого async flow тестируется crash в точках:

1. до DB transaction;
2. после domain write, до commit;
3. после commit, до publish;
4. после publish, до outbox status update;
5. после consumer side effect, до result publish;
6. после result publish, до producer confirmation.

После каждого crash/restart итог должен быть либо один корректный side effect, либо явно recoverable pending operation — никогда два conflicting результата.

## Тесты безопасности

- Invalid signature, wrong key ID/producer/subject, modified payload.
- Expired/future message and replay.
- Same `message_id` with different payload digest.
- Oversized/deep JSON, unknown envelope field, malformed base64/UTF-8.
- Forged Telegram webhook secret and callback action token.
- Log/trace snapshot test на отсутствие secrets/PII.
- NATS ACL negative tests для publish/subscribe вне разрешённых subjects.
- AES-GCM round-trip/AAD tamper, HMAC lookup, key rotation/re-encryption and forbidden test key in production.
- Configuration matrix for every Telegram mode and all fail-fast invalid combinations.
- Telegram unsafe `sendMessage` outcome-unknown test: no automatic duplicate delivery.

## Контрактные примеры

На каждый message type:

- минимальный valid payload;
- полный valid payload;
- invalid payload cases;
- signed envelope positive vector;
- one-bit modified negative vector;
- duplicate/retry expected result;
- previous minor schema compatibility fixture.

Fixtures принадлежат consumer; producer запускает их в CI. Внешним командам передаётся standalone verifier.

## Migration tests

- Empty database → latest.
- Previous released schema → latest с representative data.
- App N-1 запускается на expand-phase schema N.
- Constraints выдерживают concurrent writes.
- Rollback/recovery описан для каждой необратимой data migration.

## Приёмочный E2E-сценарий

1. External fake запрашивает invite.
2. Telegram fake проходит `/start` и отправляет wallet.
3. Top-up fake выдаёт reservation и payment claim.
4. Active-sessions повторно проверяет claim.
5. Bot показывает activation success.
6. Вторая Telegram identity активируется тем же wallet.
7. Первая сессия видит обе и отзывает вторую.
8. Отозванная сессия больше не получает active functionality.

Этот flow прогоняется в direct polling, direct webhook и natsproxy modes, когда контракт natsproxy готов.

## Проверки качества в CI

- formatting/import ordering;
- `go vet`, выбранный static analyzer и vulnerability scan;
- unit/fuzz smoke/race tests;
- integration/contract tests;
- migration lint/smoke;
- reproducible container build + image scan;
- schema compatibility check;
- проверка ссылок рабочей документации и локальной wiki для изменённых контрактов.
