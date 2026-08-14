# Контракты NATS и Ed25519

Статус: `УТВЕРЖДЁННЫЙ ВНУТРЕННИЙ КОНТРАКТ V1`; внешние команды подтверждают совместимость conformance tests или используют явно версионированный adapter.

## Принципы

- Только асинхронные publish/subscribe flows; NATS Request/Reply API не используется.
- Commands адресуются одному логическому владельцу через queue group; events могут иметь несколько подписчиков.
- Core NATS не является журналом. Значимые commands/results проходят через outbox/inbox.
- Повтор команды с тем же `message_id` обязан вернуть ранее сохранённый результат.
- Любой side effect идемпотентен по `message_id` или явному `operation_id`.
- Payload version additive-compatible внутри major version; breaking change создаёт новый subject major version.

## Именование subjects

Формат: `rw.<owner>.v<major>.<aggregate>.<action>`.

| Subject | Вид | Отправитель | Получатель |
| --- | --- | --- | --- |
| `rw.bot.v1.invite.create` | command | External initiator | rw-bot queue |
| `rw.bot.v1.invite.created` | result event | rw-bot | External initiator |
| `rw.topup.v1.activation.reserve` | command | rw-bot | External top-up queue |
| `rw.topup.v1.activation.reserved` | result event | External top-up | rw-bot |
| `rw.topup.v1.activation.reserve_rejected` | result event | External top-up | rw-bot |
| `rw.topup.v1.activation.payment_confirmed` | event | External top-up | rw-bot |
| `rw.sessions.v1.activation.verify` | command | rw-bot | active-sessions queue |
| `rw.topup.v1.activation.verify` | command | active-sessions | External top-up queue |
| `rw.topup.v1.activation.verified` | result event | External top-up | active-sessions |
| `rw.topup.v1.activation.verification_rejected` | result event | External top-up | active-sessions |
| `rw.sessions.v1.activation.activated` | result event | active-sessions | rw-bot |
| `rw.sessions.v1.activation.rejected` | result event | active-sessions | rw-bot |
| `rw.sessions.v1.session.list` | command | rw-bot | active-sessions queue |
| `rw.sessions.v1.session.listed` | result event | active-sessions | rw-bot |
| `rw.sessions.v1.session.revoke` | command | rw-bot | active-sessions queue |
| `rw.sessions.v1.session.revoked` | result event | active-sessions | rw-bot |
| `rw.sessions.v1.session.revoke_rejected` | result event | active-sessions | rw-bot |
| `rw.sessions.v1.snapshot.request` | command | rw-bot | active-sessions queue |
| `rw.sessions.v1.snapshot.page` | result event | active-sessions | rw-bot |
| `rw.sessions.v1.snapshot.completed` | result event | active-sessions | rw-bot |
| `rw.natsproxy.v1.telegram.update_received` | event | External natsproxy | rw-bot queue |
| `rw.natsproxy.v1.telegram.call_execute` | command | rw-bot | External natsproxy queue |
| `rw.natsproxy.v1.telegram.call_completed` | result event | External natsproxy | rw-bot |
| `rw.natsproxy.v1.telegram.call_rejected` | result event | External natsproxy | rw-bot |
| `rw.diagnostics.v1.incident.report` | event | Оба наших сервиса | External diagnostics |

Эти subject names являются каноническими для v1. Dynamic reply subjects не используются: они усложняют ACL и восстановление результата после рестарта.

## Envelope v1

```json
{
  "envelope_version": 1,
  "message_id": "018f...",
  "message_type": "rw.sessions.activation.verify.v1",
  "subject": "rw.sessions.v1.activation.verify",
  "producer": "rw-bot",
  "key_id": "bot-prod-2026-01",
  "occurred_at": "2026-08-10T12:00:00.123456Z",
  "expires_at": "2026-08-10T12:05:00Z",
  "correlation_id": "018f...",
  "causation_id": "018f...",
  "content_type": "application/json",
  "payload_base64": "ey4uLn0=",
  "signature_base64": "..."
}
```

| Поле | Обязательное | Правило |
| --- | --- | --- |
| `envelope_version` | Да | Только integer `1` для этого формата |
| `message_id` | Да | Глобально уникальный UUID, постоянный при retry |
| `message_type` | Да | Тип + major payload version |
| `subject` | Да | Exact NATS subject; обязан совпасть с фактическим subject доставки |
| `producer` | Да | Стабильный service identity из allowlist |
| `key_id` | Да | Идентификатор trusted Ed25519 public key |
| `occurred_at` | Да | UTC RFC3339 с фиксированным сериализатором |
| `expires_at` | Для commands | После срока command не выполняется |
| `correlation_id` | Да | Один business flow; в первом command равен operation ID |
| `causation_id` | Кроме корневого | `message_id` непосредственной причины |
| `content_type` | Да | На старте `application/json` |
| `payload_base64` | Да | Точные payload bytes; после подписи не пересериализуются |
| `signature_base64` | Да | Ed25519 signature, standard base64 without line breaks |

Unknown envelope fields отклоняются в v1, чтобы разные реализации не подписывали разные представления.

## Данные для подписи v1

Чтобы не зависеть от порядка JSON keys, подписывается не JSON envelope. Формируется бинарная последовательность:

```text
"RW-NATS-SIGNED-V1" ||
LP(envelope_version as ASCII) ||
LP(message_id UTF-8) ||
LP(message_type UTF-8) ||
LP(subject UTF-8) ||
LP(producer UTF-8) ||
LP(key_id UTF-8) ||
LP(occurred_at UTF-8) ||
LP(expires_at UTF-8 or empty) ||
LP(correlation_id UTF-8) ||
LP(causation_id UTF-8 or empty) ||
LP(content_type UTF-8) ||
LP(raw payload bytes)
```

`LP(x)` — 8-byte unsigned big-endian length followed by exactly `x`. Ed25519 подписывает эту последовательность напрямую. `signature_base64` в input не входит. Общие positive/negative test vectors являются обязательной частью v1 conformance bundle.

## Проверка входного сообщения

1. Ограничить размер NATS payload до 256 KiB.
2. Строго разобрать envelope и base64.
3. Проверить exact equality envelope `subject` с фактическим NATS subject, затем version, type и producer allowlist.
4. Найти public key по `(producer, key_id)`.
5. Проверить подпись constant-time библиотечным Ed25519 verify.
6. Проверить `occurred_at`, `expires_at` и clock skew ±2 минуты.
7. Проверить соответствие schema `message_type`.
8. В одной DB-транзакции зарегистрировать `message_id`, выполнить domain transition и сохранить result/outbox.

UUID сериализуется lowercase canonical form. Timestamp обязан быть UTC с `Z` и canonical RFC3339Nano без лишних trailing zeros; получатель parse+reformat сравнивает исходную строку. Producer при retry публикует exact сохранённые envelope bytes, а не сериализует объект заново.

Невалидное сообщение не получает business result; создаётся безопасное diagnostic событие без полного payload.

## Контракты payload

Все ID — strings с ограничениями в JSON Schema. UUID имеют UUIDv7 format. Telegram int64 IDs кодируются decimal strings, чтобы не терять точность в JavaScript. `balance_id` — UTF-8 string длиной 1–256 bytes после trim; внутри значение opaque. Денежные значения передаются decimal string с обязательным scale из deployment policy, никогда не IEEE-754 number.

### Invite

| Сообщение | Обязательные поля payload |
| --- | --- |
| `invite.create` | `operation_id`, `balance_id`, optional `requested_ttl_seconds` |
| `invite.created` | `operation_id`, `invite`, `bot_deep_link`, `expires_at` |

Повтор `invite.create` с тем же `operation_id` возвращает тот же invite. Повтор с новым ID создаёт новый single-use invite. TTL по умолчанию 24 часа; допустимый запрос — от 5 минут до 7 дней.

### Activation reservation

| Сообщение | Обязательные поля payload |
| --- | --- |
| `activation.reserve` | `operation_id`, `balance_id`, `session_id`, `sender_wallet`, `verification_amount`, `asset`, `network` |
| `activation.reserved` | `operation_id`, `external_reservation_id`, `receiver_wallet`, `amount`, `valid_from`, `expires_at` |
| `activation.reserve_rejected` | `operation_id`, `code`, `retryable`, optional `retry_after` |
| `activation.payment_confirmed` | `operation_id`, `external_reservation_id`, `transaction_id`, `sender_wallet`, `receiver_wallet`, `amount`, `observed_at` |

`payment_confirmed` — claim от top-up, но ещё не авторитетная активация.

### Activation verification

| Сообщение | Обязательные поля payload |
| --- | --- |
| `sessions.activation.verify` | `operation_id`, `session_id`, `balance_id`, `bot_id`, `telegram_user_id`, `telegram_chat_id`, expected `sender_wallet`, `receiver_wallet`, `amount`, `asset=USDT`, `network`, `transaction_id`, `external_reservation_id`, `offer_valid_from`, `offer_expires_at`; optional `display_label` |
| `topup.activation.verify` | `operation_id`, `transaction_id`, `external_reservation_id`, expected `sender_wallet`, `receiver_wallet`, `amount`, `network` |
| `topup.activation.verified` | `operation_id`, normalized transaction facts, `finalized_at` |
| `topup.activation.verification_rejected` | `operation_id`, `code`, `retryable`, optional `retry_after` |
| `sessions.activation.activated` | `operation_id`, `session_id`, `balance_id`, `status=ACTIVE`, `authority_version`, `activated_at` |
| `sessions.activation.rejected` | `operation_id`, `code`, `retryable`, optional `retry_after` |

Active-sessions обязан сравнить проверенные факты с заявленными и отдельно применить invariant первоначального sender wallet.

Поля ожидаемых фактов платежа и `bot_id` обязательны с первой реализованной версией consumer. Изменение аддитивно для wire format, но producer со старым неполным payload получает `INVALID_ARGUMENT` и должен быть обновлён до интеграции. Обоснование и compatibility impact зафиксированы в [ADR-010](adr/010-activation-verification-facts.md).

### Список и отзыв сессий

| Сообщение | Обязательные поля payload |
| --- | --- |
| `session.list` | `operation_id`, `requester_session_id`, optional opaque `cursor`, `page_size` (1–100, default 20) |
| `session.listed` | `operation_id`, `sessions[]` с `session_id`, display label, client type, activation time, current/revoked flags; optional `next_cursor` |
| `session.revoke` | `operation_id`, `requester_session_id`, `target_session_id` |
| `session.revoked` | `operation_id`, `target_session_id`, `status=REVOKED`, `authority_version`, `revoked_at` |
| `session.revoke_rejected` | `operation_id`, `code`, `retryable=false` |

Balance ID не принимается от Telegram-клиента для list/revoke: он выводится из авторитетной requester session.

### Восстановление projection

| Сообщение | Обязательные поля payload |
| --- | --- |
| `snapshot.request` | `operation_id`, optional `cursor`, `page_size` (1–100) |
| `snapshot.page` | `operation_id`, `items[]`, `next_cursor`; item содержит session identity, status, timestamps и `authority_version` |
| `snapshot.completed` | `operation_id`, `snapshot_started_at`, `total_items` |

Cursor opaque, подписан active-sessions и живёт 15 минут. Snapshot имеет границу `snapshot_started_at`: изменения после неё всё равно приходят обычными events. Bot применяет item только когда `authority_version` больше локальной; отсутствие session в snapshot не вызывает delete без completed marker и отдельной reconciliation pass.

### Diagnostics

| Поле | Назначение |
| --- | --- |
| `incident_id` | Идемпотентный идентификатор |
| `service`, `instance`, `environment` | Источник |
| `category`, `severity`, `code` | Машинная классификация |
| `summary` | Безопасное краткое описание |
| `correlation_id` | Связь с flow, если не секретна |
| `first_seen_at`, `last_seen_at`, `occurrences` | Агрегация повторов |
| `retryable`, `dependency` | Операционная реакция |

В diagnostic payload запрещены токены, private keys, invite, полный wallet, Telegram message content и произвольный SQL/error dump.

Допустимые `severity`: `INFO`, `WARNING`, `ERROR`, `CRITICAL`. Категории v1: `DEPENDENCY`, `DATABASE`, `MESSAGING`, `SECURITY`, `CONTRACT`, `DATA_CONSISTENCY`, `INTERNAL`. Минимальные codes: `DB_UNAVAILABLE`, `NATS_DISCONNECTED`, `NATS_SLOW_CONSUMER`, `TELEGRAM_UNAVAILABLE`, `TOPUP_UNAVAILABLE`, `INVALID_SIGNATURE`, `REPLAY_DETECTED`, `PAYLOAD_SCHEMA_INVALID`, `MESSAGE_ID_COLLISION`, `PROJECTION_DRIFT`, `OUTBOX_DEAD`, `UNEXPECTED_STATE`. Unknown code допускается только с category `INTERNAL` и не используется как metric label до регистрации в code catalog.

### Telegram через natsproxy

| Сообщение | Обязательные поля payload |
| --- | --- |
| `telegram.update_received` | `bot_id`, `update_id`, `received_at`, `update_json_base64` |
| `telegram.call_execute` | `operation_id`, `bot_id`, `method`, `request_json_base64`, `deadline` |
| `telegram.call_completed` | `operation_id`, `method`, `telegram_response_json_base64`, `completed_at` |
| `telegram.call_rejected` | `operation_id`, `method`, `code`, `retryable`, optional `retry_after` |

В proxy mode Telegram bot token хранит только natsproxy; `rw-bot` знает стабильный `bot_id`. Разрешённые методы v1: `getMe`, `sendMessage`, `editMessageText`, `answerCallbackQuery`, `setWebhook`, `deleteWebhook`. Остальные методы отклоняются. `update_json_base64` и response содержат точные Telegram JSON bytes, но общий signed envelope остаётся ограничен 256 KiB. natsproxy сохраняет идемпотентный result для повторного `operation_id`.

Retry policy методов: `getMe`, `editMessageText`, `answerCallbackQuery`, `setWebhook`, `deleteWebhook` безопасно повторяются с тем же operation ID; `message is not modified` считается success. `sendMessage` после начала HTTP dispatch автоматически не повторяется: timeout/connection loss переводит call в `OUTCOME_UNKNOWN`. Повтор duplicate command возвращает сохранённый status/result, а не выполняет метод заново.

## Сроки сообщений v1

| Flow | Command expiry | Поведение после expiry |
| --- | --- | --- |
| Invite create | 5 минут | `EXPIRED`; issuer создаёт новый operation ID |
| Activation reserve | 2 минуты | `TEMPORARILY_UNAVAILABLE`; пользователь может повторить позже |
| Payment claim event | 24 часа от `occurred_at`, но не позже terminal operation | Late event сохраняется как audit, сессию не активирует |
| Sessions activation verify | До `offer_expires_at + 30 минут`, максимум 2 часа | `EXPIRED`/manual review, без активации |
| Top-up verification | 5 минут на одну command; orchestration retry в пределах общего verify deadline | Повтор того же message ID |
| Session list/revoke | 2 минуты | UI показывает временную ошибку; новый user action создаёт новый operation ID |
| Session snapshot page | 5 минут, cursor 15 минут | Rebuild повторяет текущую page или начинает новый snapshot |
| natsproxy Telegram call | 30 секунд | Retry только если method policy помечена safe/idempotent |
| Diagnostic incident | 24 часа | Aggregate/drop после retention limit, основной flow не блокируется |

Events без `expires_at` в v1 запрещены: producer обязан установить срок согласно таблице. Получатель дополнительно проверяет допустимость domain transition, поэтому корректная подпись и неистёкший срок сами по себе не разрешают side effect.

## Классификация ошибок

| Класс | Примеры | Retry |
| --- | --- | --- |
| `INVALID_ARGUMENT` | Неверный формат/обязательное поле | Нет |
| `NOT_AUTHORIZED` | Requester не может list/revoke | Нет |
| `NOT_FOUND` | Invite/session/operation отсутствует | Обычно нет |
| `CONFLICT` | Wallet mismatch, запрещённый state transition | Нет без нового действия пользователя |
| `WALLET_MISMATCH` | Repeat activation использует другой canonical sender wallet | Нет; security/audit event |
| `EXPIRED` | Invite/offer/command истёк | Новая операция |
| `RATE_LIMITED` | Превышен утверждённый action limit | После `retry_after` |
| `CONTRACT_VIOLATION` | Подписанное сообщение не соответствует schema/policy | Нет; diagnostic/security incident |
| `OUTCOME_UNKNOWN` | Внешний unsafe call мог выполниться, но result потерян | Автоматически нет; reconciliation/manual policy |
| `TEMPORARILY_UNAVAILABLE` | Нет доступного receiver, dependency down | Да, с `retry_after` |
| `INTERNAL` | Неожиданная ошибка | Да по ограниченной policy + diagnostic |

## Контрактная совместимость

- Schemas и AsyncAPI хранятся в `contracts/` и версионируются вместе с кодом.
- Каждый producer обязан проходить consumer-owned fixtures.
- Breaking changes идут через новый `v2` subject; dual-publish/dual-consume используется в период миграции.
- Поля нельзя переиспользовать с новой семантикой.
- Unknown payload fields принимаются и игнорируются внутри major version; required field не удаляется.
