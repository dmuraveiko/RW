# Надёжность и масштабирование

## Модель отказов

Core NATS даёт best-effort at-most-once доставку. Offline или slow consumer может пропустить сообщение; порядок между разными publishers не гарантируется. Поэтому NATS — быстрый transport, PostgreSQL — durable workflow state. Основание: [Core NATS](https://docs.nats.io/nats-concepts/core-nats), [NATS FAQ](https://docs.nats.io/reference/faq) и [Slow Consumers](https://docs.nats.io/running-a-nats-service/nats_admin/slow_consumers).

## Протокол доставки поверх Core NATS

1. Application transaction одновременно изменяет domain state и вставляет exact envelope в outbox.
2. Outbox worker выбирает сообщения batch-ами с `FOR UPDATE SKIP LOCKED`.
3. После успешного NATS publish запись становится `PUBLISHED`, но не считается business-confirmed.
4. Пока terminal result не получен или command не истёк, publisher повторяет тот же `message_id` с bounded exponential backoff + jitter.
5. Consumer в одной транзакции регистрирует inbox, выполняет side effect и сохраняет result в outbox.
6. Повтор команды с тем же ID не выполняет side effect: consumer заново публикует тот же сохранённый result.
7. Result consumer идемпотентно завершает исходную operation и помечает command confirmed.
8. Reconciliation worker периодически находит зависшие операции и повторяет command либо переводит их в ручное/terminal состояние по policy.

Это даёт eventual business completion при условии, что обе стороны в какой-то момент снова доступны. Оно не превращает Core NATS в durable broker и требует ограниченного хранения outbox/inbox.

Статус outbox зависит от вида сообщения:

- command становится `CONFIRMED` только после correlated terminal result;
- result после успешного publish остаётся доступен как `PUBLISHED` и повторяется при duplicate исходного command;
- notification event считается transport-published после publish, а потеря компенсируется snapshot/reconciliation протоколом;
- diagnostic event — best effort с bounded retry до expiry и не блокирует business flow.

## Конкурентная обработка

- Все команды могут попасть на любую replica через NATS queue group.
- Domain invariants защищаются unique/check constraints, conditional update по `version` и точечными row locks.
- Первая установка sender wallet сериализуется на записи `balance_identities`; конкурент с другим wallet получает deterministic conflict.
- Invite consumption — один `UPDATE ... WHERE status='ISSUED' AND expires_at > now()` с проверкой affected rows.
- State transition содержит expected current state/attempt ID; старое событие не может активировать новую попытку.
- Outbox/inbox workers работают batch-ами; долгий сетевой publish не выполняется внутри business transaction.

## Порядок событий

Глобальный порядок не предполагается. Для каждой aggregate используются:

- `operation_id` для одного flow;
- `causation_id` для причинной цепочки;
- monotonic `version` authoritative session для projection;
- допустимые state transitions;
- timestamp только для expiry, не для определения порядка конкурентных updates.

## Доставка Telegram updates

- `update_id` дедуплицируется до side effects.
- Webhook отвечает 2xx только после durable acceptance update; тяжёлая обработка выполняется отдельно.
- При long polling offset продвигается только после durable acceptance.
- Повтор отправки пользователю имеет deterministic notification key, чтобы рестарт не порождал каскад одинаковых сообщений.
- natsproxy v1 передаёт исходный Telegram `update_id`, который используется как stable idempotency key.

## Деградация зависимостей

| Зависимость | Поведение |
| --- | --- |
| PostgreSQL недоступна | Не принимать durable commands; readiness false; Telegram получает безопасный временный ответ, если это возможно без state mutation |
| NATS недоступен | Локальные Telegram inputs сохраняются; outbox ждёт reconnect; операции показываются pending |
| Telegram API недоступен | State сохраняется; notifications остаются pending; diagnostic агрегируется |
| Top-up недоступен | Новая activation получает retry-later или pending по SLA; активной не становится |
| Active-sessions недоступен | Bot сохраняет verification pending и повторяет; не активирует локально самостоятельно |
| Diagnostics недоступен | Ошибка логируется/метрится, diagnostic outbox ограничен TTL/размером, основной flow не блокируется |

## Horizontal scaling

- [NATS queue groups](https://docs.nats.io/nats-concepts/core-nats/queue) балансируют commands между replicas.
- HTTP webhook replicas стоят за load balancer; update dedup в общей Bot DB.
- Outbox workers используют `SKIP LOCKED`.
- Connection pools имеют per-replica limits, рассчитанные от общего PostgreSQL budget.
- Горячие aggregates блокируются только на короткую транзакцию; внешняя сеть никогда не вызывается под DB lock.
- Projection/list pagination обязательна до выхода на production; message payload не содержит неограниченные массивы.

## Обратное давление и перегрузка

- Ограниченные NATS pending buffers и async error callbacks.
- Bounded worker pools и batch sizes.
- Rate limits по Telegram user/chat, balance ID и invite issuer.
- Outbox lag, oldest pending age и slow-consumer errors являются alerts.
- При перегрузке новые несущественные действия отклоняются `TEMPORARILY_UNAVAILABLE`; подтверждённое durable state не теряется.

## Утверждённые стартовые SLO

Численные SLO утверждены в [technical-decisions.md](technical-decisions.md). Обязательные показатели: availability durable command acceptance; p95/p99 Telegram processing; invite/activation latency; stale operation share; outbox/inbox errors and duplicates; NATS reconnect/slow-consumer; DB pool/lock waits; Telegram 429/5xx.

## Аварийное восстановление

- PostgreSQL backups + PITR для обеих БД.
- Проверяемый restore runbook минимум на staging.
- После restore outbox/reconciliation восстанавливают незавершённые flows; inbox защищает от повторного side effect.
- Потерянная bot projection перестраивается через утверждённый paginated session snapshot protocol; rebuild идемпотентно применяет только большую `authority_version`.
