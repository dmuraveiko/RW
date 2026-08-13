# Реестр закрытых технических вопросов

Статус: `ЗАКРЫТО`  
Дата решения: `2026-08-10`

Все технические вопросы P0–P2 закрыты решениями команды. Детальные значения находятся в [утверждённых технических решениях](technical-decisions.md), wire protocol — в [контрактах](contracts.md), таблицы — в [модели данных](data-model.md).

## P0

| № | Вопрос | Решение |
| --- | --- | --- |
| 1 | Границы сервисов и развёртывания | Два отдельных бинарника, deployment и PostgreSQL database в одном monorepo; общей схемы нет |
| 2 | Владение active state | Active-sessions — единственный authority; bot хранит только projection |
| 3 | NATS contracts | Fixed v1 subjects, JSON Schema, signed envelope, UUIDv7 correlation, fixed result events и error taxonomy |
| 4 | Надёжность Core NATS | Transactional inbox/outbox, повтор того же message ID до terminal result/expiry, reconciliation |
| 5 | Ed25519 | Length-prefixed signature input v1; PKCS#8/PKIX PEM keys; key ID, expiry, replay inbox, rotation overlap |
| 6 | Top-up integration | Reserve, payment claim и independent final verification contracts; активируется только `VERIFIED_FINAL` |
| 7 | Telegram/natsproxy | `direct_polling`, `direct_webhook`, `natsproxy`; production default — webhook, один mode одновременно |

## P1

| № | Вопрос | Решение |
| --- | --- | --- |
| 8 | USDT network и wallet | Одна обязательная network на deployment без default; address проходит network validator, хранится canonical value |
| 9 | Invite | 256-bit base64url, digest-only storage, single-use, default TTL 24h, диапазон 5m–7d |
| 10 | Telegram session identity | `(bot_id, telegram_user_id)` определяет Telegram identity, UUIDv7 — конкретную binding; одновременно одна active/non-terminal binding, после revoke возможна новая |
| 11 | Sender wallet invariant | Один `(network, canonical wallet)` на balance ID; смена wallet вне текущего scope |
| 12 | List/revoke authorization | Authority проверяет active requester и общий balance; soft revoke; self/last revoke разрешены с confirmation |
| 13 | Session list data | Optional sanitized label, short session ID, activation date, current marker; без полного чужого Telegram ID |
| 14 | Activation amount/TTL/late payments | Integer minor units, external deadline 1–60m, exact verified facts; late/partial/excess не активируют |
| 15 | Bot UX | Русский язык первой версии; `/start`, ручной invite, activation, «Мои сессии», revoke confirmation; тексты отделены от domain |

## P2

| № | Вопрос | Решение |
| --- | --- | --- |
| 16 | Версии и tooling | Go 1.26.5, PostgreSQL 18.4, NATS 2.14.3, pgx 5.10, Goose 3.27, Compose + Helm, OTel/Prometheus |
| 17 | Diagnostics | Signed `incident.report`, severity/category/code, агрегирование и bounded outbox; outage не блокирует flow |
| 18 | Load/retention/privacy/DR | Bounded pools/rate limits; утверждён retention; PII redaction/HMAC; PITR RPO 5m/RTO 60m |
| 19 | Local tests | Docker Compose для ручного запуска, Testcontainers для CI/integration, fakes для всех внешних сервисов |

## Deployment inputs, а не открытые вопросы

Перед конкретным production-развёртыванием обязательно предоставляются:

- `RW_USDT_NETWORK` и параметры verification amount;
- Telegram bot token и webhook public URL;
- NATS URLs/credentials/CA и subject ACL;
- PostgreSQL DSNs и pool budget;
- Ed25519 private key, trusted public-key registry и AES result-encryption keyring;
- production hostnames, OTLP/metrics endpoints и secret-manager paths.

Отсутствие любого обязательного input приводит к fail-fast; сервис не запускается в production с guessed defaults.
