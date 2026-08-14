# Результат итерации II-A

Дата: `2026-08-14`

Статус: `ГОТОВО ДЛЯ ЛОКАЛЬНОЙ ДЕМОНСТРАЦИИ И ИНТЕГРАЦИИ`

## Результат

Реализован первый прикладной вертикальный срез active-sessions поверх Core NATS:

- signed `sessions.activation.verify` command;
- независимый запрос `topup.activation.verify`;
- обработка verified/rejected result;
- атомарное создание balance identity и active session;
- неизменяемый sender wallet первой подтверждённой активации;
- повторная активация тем же wallet;
- `WALLET_MISMATCH` для другого wallet;
- повтор сохранённого result для duplicate command;
- отказ без side effect для late finalized result.

## Надёжность

- inbox/outbox находятся в PostgreSQL;
- exact signed outbox envelopes зашифрованы AES-256-GCM;
- publish выполняется вне domain transaction;
- queue consumers и publisher имеют bounded concurrency/backpressure;
- command повторяется с exponential backoff до terminal result/expiry;
- stale top-up command заменяется новым signed command в пределах общего offer deadline;
- late result не активирует session;
- same message ID с другим payload digest отклоняется.

## Проверенный demo

```bash
make dev-up
make demo-active-sessions
```

Два последовательных запуска подтверждают отсутствие конфликтов между независимыми operations. На чистых PostgreSQL volumes обе схемы обновляются до версии 2, оба сервиса проходят readiness, а outbox и application logs не содержат открытых wallets. Тот же сценарий включён в CI отдельной integration job.

## Ограничения результата

- top-up представлен локальным fake, production conformance ещё требуется;
- list/revoke и snapshot не входят в этот срез;
- bot ещё не создаёт activation command из Telegram dialog;
- полная crash/chaos/retention/metrics matrix остаётся до закрытия этапов 2–3.
