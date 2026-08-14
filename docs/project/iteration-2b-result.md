# Результат итерации II-B

Дата: `2026-08-14`

Статус: `TELEGRAM ACTIVATION FLOW ЗАВЕРШЁН`

## Результат

Реализован сквозной сценарий:

1. Внешний issuer публикует подписанную `invite.create`.
2. Bot сохраняет одноразовый invite и возвращает deep link.
3. `/start <invite>` атомарно создаёт inactive Telegram session.
4. Sender wallet запускает durable activation reservation.
5. После `payment_confirmed` bot отправляет `sessions.activation.verify`.
6. Active-sessions независимо перепроверяет top-up и применяет sender-wallet invariant.
7. Только авторитетный result создаёт active projection и уведомляет пользователя.

Состояния и внешние команды сохраняются в PostgreSQL. Balance ID, invite token, sender/receiver wallets и NATS outbox зашифрованы; поиск выполняется по digest/HMAC fingerprint.

## Проверка

```bash
make dev-up
make demo-telegram-flow
```

Сценарий создаёт invite через NATS, имитирует два Telegram-сообщения, проходит reserve/payment/verification через reference fake top-up и проверяет конечный `ACTIVE` в Bot DB.

## Следующая вертикаль

`session.list` и `session.revoke`, включая авторизацию active-sessions, Telegram presentation и projection updates. Rate limits, durable Telegram delivery queue, webhook/natsproxy и production top-up conformance остаются отдельным hardening.
