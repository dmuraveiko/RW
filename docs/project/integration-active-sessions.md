# Интеграция с activation verification

Статус: `ДОСТУПНО ДЛЯ ЛОКАЛЬНОЙ ИНТЕГРАЦИИ`

Версия контракта: `v1`

## Что реализовано

`rw-active-sessions` принимает подписанную команду `rw.sessions.v1.activation.verify`, независимо запрашивает fake/внешний top-up через `rw.topup.v1.activation.verify` и публикует один из терминальных результатов:

- `rw.sessions.v1.activation.activated`;
- `rw.sessions.v1.activation.rejected`.

Все взаимодействия выполняются через Core NATS Publish/Subscribe. Dynamic reply subjects и NATS Request/Reply не используются.

## Быстрый запуск

```bash
make dev-up
make demo-active-sessions
```

Demo выполняет пять проверок:

1. первая verified activation создаёт balance identity и active session;
2. повтор того же NATS command возвращает тот же result message без нового side effect;
3. новая Telegram-сессия того же balance ID активируется тем же sender wallet;
4. другой sender wallet получает `WALLET_MISMATCH`.
5. поздно финализированный top-up result получает `CONTRACT_VIOLATION` и не активирует session.

После сценария команда выводит агрегированное состояние `active_sessions` и `activation_verifications`. Demo использует только локальные test keys и `fixture-net`.

## Контракты

- AsyncAPI: [`contracts/asyncapi/realwallet-v1.yaml`](../../contracts/asyncapi/realwallet-v1.yaml)
- Payload schemas: [`contracts/schemas/payloads-v1.schema.json`](../../contracts/schemas/payloads-v1.schema.json)
- Signed envelope: [`contracts/schemas/envelope-v1.schema.json`](../../contracts/schemas/envelope-v1.schema.json)
- Пример входной команды: [`contracts/fixtures/sessions-activation-verify-v1.json`](../../contracts/fixtures/sessions-activation-verify-v1.json)
- Пример ответа top-up: [`contracts/fixtures/topup-activation-verified-v1.json`](../../contracts/fixtures/topup-activation-verified-v1.json)

Обязательные правила producer:

- один business operation сохраняет `operation_id` при retry;
- один exact command сохраняет `message_id` и подписанный envelope при повторной публикации;
- `subject` внутри envelope совпадает с фактическим NATS subject;
- command содержит `expires_at`, а timestamps имеют canonical UTC RFC3339Nano format;
- payload не логируется целиком: он содержит balance ID и wallets;
- producer принимает повтор одного и того же terminal result.

## Надёжность и безопасность

- command регистрируется в PostgreSQL inbox до side effect;
- команда top-up и terminal result создаются транзакционно в outbox;
- command повторяется, пока не получен terminal result или не истёк срок;
- duplicate command с тем же digest переиспользует сохранённый result;
- тот же `message_id` с другим digest отклоняется как collision;
- входящие envelopes проверяются по subject, message type, producer allowlist, expiry и Ed25519 signature;
- balance ID и wallets хранятся зашифрованными AES-256-GCM, поиск выполняется по HMAC fingerprint;
- outbox envelopes также зашифрованы в PostgreSQL и расшифровываются только перед publish;
- первый verified sender wallet закрепляется атомарно и не меняется автоматически.

## Пока не реализовано

- production top-up adapter: demo использует reference fake;
- `session.list` и `session.revoke` consumers;
- bot-side создание verification command;
- invite/inactive-session flow;
- длительная reconciliation после истечения одной top-up verification command;
- production NATS ACL deployment и diagnostics publisher.
