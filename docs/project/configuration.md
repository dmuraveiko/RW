# Конфигурация сервисов

Статус: `УТВЕРЖДЕНО ДЛЯ V1`

Все параметры читаются один раз при старте. Unknown variables с префиксом `RW_` считаются ошибкой, кроме явно документированных platform metadata. Duration использует Go format (`250ms`, `30s`, `24h`), списки — comma-separated. В production secret value задаётся через переменную `*_FILE`; direct secret env разрешён только в `local/test`.

## Общие параметры

| Переменная | Обязательность/default | Назначение |
| --- | --- | --- |
| `RW_ENVIRONMENT` | required: `local/test/staging/prod` | Environment policy |
| `RW_INSTANCE_ID` | required prod; hostname default local | Instance/pod identity |
| `RW_LOG_LEVEL` | `info` | `debug/info/warn/error` |
| `RW_HTTP_ADDR` | `:8080` | Health/webhook listener |
| `RW_METRICS_ADDR` | `:9090` | Internal metrics listener |
| `RW_SHUTDOWN_TIMEOUT` | `30s` | Graceful drain bound |
| `RW_OTEL_EXPORTER_OTLP_ENDPOINT` | optional | OTLP endpoint |
| `RW_OTEL_SAMPLE_RATIO` | `0.1`, errors always | Trace sampling |

## PostgreSQL

| Переменная | Обязательность/default | Назначение |
| --- | --- | --- |
| `RW_DATABASE_URL_FILE` | required prod | Secret file with DSN |
| `RW_DATABASE_URL` | local/test only | Direct DSN |
| `RW_DB_POOL_MAX` | `20` | Max connections per replica |
| `RW_DB_POOL_MIN` | `2` | Min connections per replica |
| `RW_DB_STATEMENT_TIMEOUT` | `5s` | Server-side statement timeout |
| `RW_DB_LOCK_TIMEOUT` | `1s` | Lock wait limit |
| `RW_DB_IDLE_TX_TIMEOUT` | `10s` | Idle transaction limit |

Bot и active-sessions получают разные DSN/users/databases. Startup проверяет server major `18`, TLS policy и schema compatibility.

## NATS transport

| Переменная | Обязательность/default | Назначение |
| --- | --- | --- |
| `RW_NATS_URLS` | required | Comma-separated seed URLs |
| `RW_NATS_CREDS_FILE` | required staging/prod | NKEY/JWT credentials |
| `RW_NATS_TLS_CA_FILE` | required staging/prod | Trusted CA |
| `RW_NATS_TLS_SERVER_NAME` | required staging/prod | Certificate verification name |
| `RW_NATS_CONNECT_TIMEOUT` | `3s` | Initial connect timeout |
| `RW_NATS_RECONNECT_WAIT` | `1s` | Base reconnect wait; client jitter enabled |
| `RW_NATS_MAX_RECONNECTS` | `-1` | Infinite reconnect while process alive |
| `RW_NATS_RECONNECT_BUFFER` | `8MiB` | Bounded client buffer; outbox remains authority |
| `RW_NATS_PENDING_MESSAGES` | `4096` | Per-subscription pending limit |
| `RW_NATS_PENDING_BYTES` | `16MiB` | Per-subscription byte limit |

Slow-consumer/reconnect callbacks обязательны и публикуют metrics/diagnostics. NATS client buffer не считается durable storage.

## Подпись и шифрование

| Переменная | Обязательность/default | Назначение |
| --- | --- | --- |
| `RW_SIGNING_KEY_ID` | required | Current Ed25519 key ID |
| `RW_SIGNING_PRIVATE_KEY_FILE` | required prod | PKCS#8 PEM private key |
| `RW_SIGNING_PRIVATE_KEY_BASE64` | local/test only | Direct test key |
| `RW_TRUSTED_KEYS_FILE` | required | JSON registry of producer/key ID/PKIX public key/validity |
| `RW_DATA_KEYRING_FILE` | required | AES-256-GCM keys, one active + decrypt-only keys |
| `RW_FINGERPRINT_KEY_FILE` | required | Independent 256-bit HMAC key |
| `RW_CLOCK_SKEW` | fixed `2m` | Not overridable in prod without new ADR |
| `RW_MESSAGE_MAX_BYTES` | fixed `262144` | Application envelope limit |

Startup проверяет file permissions, key length/type, unique key IDs, validity overlap и запрет test keys в staging/prod.

`RW_TRUSTED_KEYS_FILE` использует JSON v1:

```json
{
  "version": 1,
  "keys": [{
    "producer": "rw-bot",
    "key_id": "bot-prod-2026-01",
    "public_key_pem": "-----BEGIN PUBLIC KEY-----...",
    "valid_from": "2026-08-01T00:00:00Z",
    "valid_until": "2027-02-01T00:00:00Z"
  }]
}
```

`RW_DATA_KEYRING_FILE` использует JSON v1 с одним `active_key_id`; `key_base64` декодируется ровно в 32 bytes:

```json
{
  "version": 1,
  "active_key_id": "data-prod-2026-01",
  "keys": [{
    "key_id": "data-prod-2026-01",
    "key_base64": "<base64-32-bytes>",
    "decrypt_only": false
  }]
}
```

Fingerprint file содержит standard base64 ровно 32 random bytes. Secret files должны быть regular files, не быть group/world-writable и не следовать symlink за пределы разрешённого secret mount.

## Bot service

| Переменная | Обязательность/default | Назначение |
| --- | --- | --- |
| `RW_TELEGRAM_MODE` | required | `direct_polling/direct_webhook/natsproxy`; `disabled` разрешён только local/test для запуска платформенного каркаса |
| `RW_TELEGRAM_BOT_ID` | required staging/prod и non-polling; optional local polling | Stable numeric bot ID; local polling сверяет/получает через `getMe` |
| `RW_TELEGRAM_BOT_USERNAME` | required staging/prod и non-polling; optional local polling | Deep-link generation/validation; local polling сверяет/получает через `getMe` |
| `RW_TELEGRAM_TOKEN_FILE` | direct modes, required prod | Bot API token secret |
| `RW_TELEGRAM_TOKEN` | direct modes local only | Direct token |
| `RW_TELEGRAM_WEBHOOK_PUBLIC_URL` | webhook only | HTTPS endpoint |
| `RW_TELEGRAM_WEBHOOK_SECRET_FILE` | webhook only | Header secret |
| `RW_TELEGRAM_POLL_TIMEOUT` | `30s` | Long-poll timeout |
| `RW_INVITE_TTL` | `24h` | Default invite TTL |
| `RW_INVITE_TTL_MIN/MAX` | fixed `5m/168h` | Issuer request bounds |
| `RW_USDT_NETWORK` | required | Exact network code agreed with top-up |
| `RW_USDT_SCALE` | required | Decimal scale |
| `RW_ACTIVATION_AMOUNT_MIN_MINOR` | required | Inclusive integer minor units |
| `RW_ACTIVATION_AMOUNT_MAX_MINOR` | required | Inclusive integer minor units |
| `RW_ACTIVATION_OFFER_MIN/MAX` | fixed `1m/60m` | Accepted top-up deadline bounds |
| `RW_TELEGRAM_RATE_PER_MINUTE/BURST` | `30/10` | Общий ingress rate limit |
| `RW_INVALID_INVITE_LIMIT/WINDOW/BLOCK` | `5/10m/30m` | Brute-force protection |
| `RW_ACTIVATION_RATE_IDENTITY_HOURLY` | `3` | Reservation attempts per Telegram identity |
| `RW_ACTIVATION_RATE_BALANCE_HOURLY` | `10` | Reservation attempts per balance ID |

В `natsproxy` mode Telegram token/webhook variables запрещены: credential принадлежит proxy. Amount range должен содержать достаточно значений для ожидаемой concurrency; startup отклоняет диапазон меньше `1000` вариантов.

Режим `disabled` не является production transport: он не принимает Telegram updates и нужен только для `make dev-up`, когда проверяется платформа без BotFather token. На текущем этапе фактически реализован `direct_polling`. Выбор `direct_webhook` или `natsproxy` проходит config validation, но startup явно завершается ошибкой `not implemented`, пока соответствующий adapter не добавлен.

## Active-sessions service

| Переменная | Обязательность/default | Назначение |
| --- | --- | --- |
| `RW_USDT_NETWORK` | required | Must equal bot/top-up deployment policy |
| `RW_SESSION_LIST_PAGE_SIZE` | `20` | Default page size |
| `RW_SESSION_LIST_PAGE_MAX` | fixed `100` | Hard maximum |
| `RW_VERIFICATION_DEADLINE_MAX` | fixed `2h` | Overall verification bound |

## Workers и retention

| Переменная | Default | Назначение |
| --- | --- | --- |
| `RW_OUTBOX_BATCH_SIZE` | `100` | Rows per claim |
| `RW_OUTBOX_CONCURRENCY` | `8` | Publishes per replica |
| `RW_OUTBOX_RETRY_INITIAL/MAX` | `250ms/30s` | Exponential retry bounds |
| `RW_RECONCILE_INTERVAL` | `30s` | Stale workflow scan |
| `RW_RECONCILE_BATCH_SIZE` | `100` | Rows per scan |
| `RW_RETENTION_INTERVAL` | `10m` | Cleanup cadence |
| `RW_RETENTION_BATCH_SIZE` | `500` | Rows per cleanup transaction |

Retention durations из [technical-decisions.md](technical-decisions.md) являются defaults и могут переопределяться только документированными `RW_RETENTION_*` variables в сторону утверждённой юридической policy.

## Проверка конфигурации

Сервис завершается до открытия readiness при любой ошибке: отсутствующий required value, взаимоисключающие Telegram modes, direct secret в production env, неверный key, одинаковый signing/encryption/fingerprint material, non-TLS external URL, min > max, неподдерживаемая network, несовместимая schema или PostgreSQL/NATS version.
