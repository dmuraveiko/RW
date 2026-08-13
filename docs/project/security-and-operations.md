# Безопасность и эксплуатация

## Границы доверия

| Граница | Угрозы | Меры защиты |
| --- | --- | --- |
| Telegram → bot | forged webhook, duplicate update, spam | Webhook secret, TLS, update dedup, rate limit |
| NATS → service | spoofed producer, tampering, replay | NATS auth/ACL/TLS + application Ed25519 + inbox/expiry |
| Service → PostgreSQL | credential leak, SQL injection | Secret manager, TLS where needed, parameterized SQL, least privilege |
| Invite URL | guessing, leakage, reuse | 256-bit random token, digest storage, TTL, atomic single use, redaction |
| Wallet/balance identity | privacy leakage, wrong normalization | Opaque handling, contract validator, masked telemetry, access control |
| Operator/admin | excessive access | Separate roles, audited migrations, no secrets in dashboards/logs |

Ed25519 application signatures не заменяют TLS, NATS authentication и subject ACL: они решают разные угрозы.

## Утверждённая политика invite

- 32 cryptographically random bytes, unpadded base64url (43 characters).
- В Telegram `start` попадает только token, без balance ID.
- В БД хранится SHA-256 digest; lookup использует digest входного token.
- Single-use consumption выполняется атомарно вместе с созданием inactive session.
- Истёкший/использованный token возвращает одинаково нейтральную ошибку.
- Значение никогда не попадает в structured logs, metrics labels, traces или diagnostics.
- Recoverable invite result и wallet values шифруются AES-256-GCM; searchable equality использует отдельный HMAC-SHA-256 fingerprint key.

TTL по умолчанию 24 часа, допустимый диапазон 5 минут–7 дней. Несколько invites на один balance ID разрешены; каждый token single-use, а Telegram identity одновременно имеет только одну non-terminal/active binding.

Telegram разрешает `start` parameter длиной до 64 base64url characters: [Telegram deep linking](https://core.telegram.org/bots/features#deep-linking).

## Управление ключами

- Private Ed25519 key поступает через secret file/manager; env допустим локально, но не рекомендуется production.
- Trusted public keys конфигурируются как `(producer, key_id, public_key, valid_from, valid_until)`.
- Rotation: сначала deploy consumers с новым public key, затем producer переключается на новый key ID, потом старый key удаляется после максимального replay/retry window.
- Unknown/expired key ID отклоняется и создаёт security diagnostic.
- Test keys отделены от production; fixtures явно помечены и не принимаются production config validator.

## Авторизация

- Invite creation доступен только NATS principal разрешённого initiator subject.
- Session list/revoke принимает requester session ID, а balance identity выводит внутри active-sessions.
- Requester должен быть ACTIVE и принадлежать тому же balance identity, что target.
- Self-revoke и отзыв последней сессии разрешены после явного confirmation; active-sessions остаётся единственным authorization authority.
- Bot callbacks содержат opaque short-lived action token, а не доверенный session ID из текста кнопки.

## Работа с секретами и персональными данными

Не логируются:

- Telegram bot token;
- Ed25519 private key/seed;
- raw invite;
- полный sender/receiver wallet;
- Telegram message body;
- NATS credentials;
- PostgreSQL DSN с credentials.

Для correlation используются внутренние IDs. Wallet/Telegram ID в диагностике маскируется или заменяется keyed fingerprint по утверждённой policy.

## Группы runtime-конфигурации

| Группа | Примеры |
| --- | --- |
| Identity | service name, environment, instance/pod metadata |
| PostgreSQL | URL/secret file, pool limits, statement/lock timeouts |
| NATS | seed URLs, credentials, TLS CA/server name, reconnect/buffer limits, queue group |
| Signing | private key path, producer ID, key ID, trusted-key manifest |
| Telegram | mode, token secret, webhook public URL/secret or polling settings |
| Workflow | invite TTL, activation/retry/reconciliation intervals, retention |
| Observability | log level, OTLP endpoint, metrics listener |

Config парсится один раз, валидируется и печатается только в redacted виде. Unknown env с project prefix может считаться ошибкой, чтобы ловить опечатки deployment.

## HTTP endpoints

| Endpoint | Доступ | Назначение |
| --- | --- | --- |
| `/health/live` | Internal | Process/event loop жив |
| `/health/ready` | Internal | DB ready, migrations compatible, subscriptions active |
| `/metrics` | Internal | Prometheus format |
| `/telegram/webhook` | Public only in webhook mode | Telegram updates; secret header required |

Debug/pprof выключен по умолчанию и доступен только на отдельном internal listener при явной настройке.

## Наблюдаемость

- JSON logs с `service`, `environment`, `message_id`, `correlation_id`, `operation_id`, безопасным `error_code`.
- Traces переходят через NATS envelope metadata, но подпись не зависит от vendor tracing headers.
- Метрики не используют balance/session/message IDs как labels.
- Diagnostic события агрегируются по code/dependency, чтобы outage не создавал DDoS диагностики.

## Развёртывание и миграции

- Immutable OCI images, non-root user, read-only filesystem кроме явно заданных temp paths.
- Separate service accounts и DB roles для runtime/migrations.
- Forward-compatible expand/migrate/contract migrations; destructive contract — только после полного rollout.
- Startup не применяет миграции автоматически в production.
- Graceful shutdown: stop intake, drain subscriptions/HTTP, finish bounded transactions, leave unpublished data in outbox.
- Rollback приложения допускается только пока schema остаётся backward compatible.

## Минимальные runbooks

- NATS disconnect/slow consumer/outbox lag.
- PostgreSQL unavailable/pool saturation/lock contention.
- Telegram webhook misconfiguration, 429 и token rotation.
- Ed25519 invalid-signature spike и key rotation rollback.
- Top-up unavailable/stuck activation reconciliation.
- Projection drift/rebuild.
- Database backup restore and post-restore reconciliation.
