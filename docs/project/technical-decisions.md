# Утверждённые технические решения

Статус: `УТВЕРЖДЕНО ДЛЯ РЕАЛИЗАЦИИ`  
Версия: `1.0`  
Дата: `2026-08-10`

Этот документ закрывает технические вопросы проекта. Решения обязательны для первой реализации. Изменение выполняется отдельным ADR с миграционным и compatibility-планом.

## 1. Платформа и зависимости

| Область | Решение |
| --- | --- |
| Go | `1.26.5`; `go` и `toolchain` фиксируются в `go.mod` |
| Module path | `github.com/dmuraveiko/RW` |
| PostgreSQL | `18.4`, отдельная БД и runtime role для каждого сервиса |
| NATS Server | `2.14.3`, только Core NATS; JetStream выключен и не используется |
| NATS client | `github.com/nats-io/nats.go v1.52.0` |
| PostgreSQL client | `github.com/jackc/pgx/v5 v5.10.0`, native API и `pgxpool`, без ORM |
| Миграции | `github.com/pressly/goose/v3 v3.27.1`, только SQL migrations |
| Integration tests | `github.com/testcontainers/testcontainers-go v0.42.0` |
| UUID | UUIDv7 через `github.com/google/uuid`; версия фиксируется в `go.mod` |
| Логи | Стандартный `log/slog`, JSON handler |
| Метрики | Prometheus client; endpoint `/metrics` только во внутренней сети |
| Трассировка | OpenTelemetry OTLP; отсутствие collector не блокирует сервис |
| HTTP | Стандартный `net/http` и `http.ServeMux`; сторонний router не нужен |
| Telegram | Собственный минимальный Bot API adapter на `net/http`, без Telegram framework |
| JSON | `encoding/json`; строгий envelope decoder, versioned payload DTO |
| Деньги | В домене integer minor units + явный scale; в wire decimal string; `float` запрещён |
| Тестовые сравнения | Стандартный `testing` + `go-cmp`; mock generation не используется |
| Защита чувствительных данных | AES-256-GCM keyring для application-level encryption + HMAC-SHA-256 lookup fingerprints |

Dependencies фиксируются в `go.mod/go.sum`. Renovate создаёт еженедельные PR. Patch/minor update проходит полный CI; major update требует ADR. Production images используют digest-pinned base images.

Baseline выбран по текущим стабильным upstream releases: [Go downloads](https://go.dev/dl/), [PostgreSQL versioning](https://www.postgresql.org/support/versioning/), [NATS Server releases](https://github.com/nats-io/nats-server/releases), [nats.go](https://github.com/nats-io/nats.go), [pgx](https://github.com/jackc/pgx), [Goose](https://github.com/pressly/goose) и [Testcontainers for Go](https://github.com/testcontainers/testcontainers-go/releases).

## 2. Сервисы и репозиторий

- Один monorepo, два независимо запускаемых бинарника: `rw-bot` и `rw-active-sessions`.
- У каждого сервиса собственные конфигурация, PostgreSQL database, migration set, NATS account/user и deployment.
- Общий код ограничен platform packages и утверждённым wire protocol. Доменные пакеты не разделяются.
- Active-sessions является единственным источником истины для `ACTIVE/REVOKED`, первоначального sender wallet и activation transaction.
- Bot DB хранит invite, inactive flow, activation attempt и read projection active status. Projection не используется для destructive authorization или авторитетного списка.
- Межсервисный доступ к чужой БД и распределённые транзакции запрещены.

## 3. Telegram-сессия

- Telegram-сессия идентифицируется парой `(bot_id, telegram_user_id)`; `chat_id` — адрес доставки, а не identity.
- Поддерживаются только private chats. Group/channel updates игнорируются и не создают сессию.
- Внутренний `session_id` — UUIDv7 одной конкретной binding. Он стабилен в ходе initial/retry activation этой binding.
- У одной Telegram identity одновременно может быть только одна non-terminal/active binding. После expiry/revoke новая valid invite создаёт новый `session_id` и может указывать другой `balance_id`; старая binding остаётся в audit.
- Один `balance_id` может иметь любое число Telegram identities с учётом rate/capacity limits.
- Username/first name не являются identity. Для списка хранится optional sanitized display label; пользователю показываются label, короткий session ID, дата активации и отметка текущей сессии.

## 4. Invite

- Token: 32 bytes из `crypto/rand`, unpadded base64url, длина 43 символа.
- В URL находится только token; `balance_id` в Telegram link не раскрывается.
- В PostgreSQL хранится SHA-256 digest token, raw token возвращается только один раз в `invite.created`.
- Invite single-use и атомарно потребляется вместе с созданием inactive session.
- TTL по умолчанию — 24 часа; issuer может запросить от 5 минут до 7 дней.
- Повтор `invite.create` с тем же `operation_id` возвращает тот же token/result из зашифрованного result store до истечения result retention. Новый operation ID создаёт новый invite.
- Чтобы поддержать идемпотентный повтор без постоянного хранения raw token, result payload в outbox хранится зашифрованным ключом приложения до `expires_at + 24h`, затем криптографически удаляется/очищается.
- Expired, consumed и unknown invite дают одинаковый пользовательский ответ.
- Неиспользованные terminal invite records удаляются через 30 дней; audit без token/digest хранится 180 дней.

## 5. Wallet, asset и network

- Asset первой версии — `USDT`.
- Network не угадывается и не имеет default. `RW_USDT_NETWORK` — обязательная production-конфигурация и часть каждого top-up message.
- Сервис поддерживает одну сеть на deployment. Поддержка нескольких сетей требует отдельной версии контракта/UI.
- Canonicalization/validation выполняется network-specific validator. До появления library/contract владельца top-up validator работает в режиме strict external policy: ASCII, длина 16–128, без whitespace/control characters; top-up verification возвращает canonical address.
- Авторитетный sender wallet сохраняется как `(network, canonical_address)` на уровне `balance_id`.
- Первый verified activation атомарно закрепляет wallet. Повторная активация допускается только с точным canonical match.
- Автоматическая смена wallet отсутствует. Recovery/change-wallet — отдельная будущая задача.
- В telemetry используется HMAC-SHA-256 fingerprint с отдельным observability key; полный wallet не логируется.

## 6. Активация

- Bot генерирует verification amount криптографически равномерно через `crypto/rand` в inclusive диапазоне integer minor units из обязательной конфигурации. Scale обязателен и сверяется с top-up contract; диапазон должен содержать минимум 1000 значений. Amount сохраняется до publish и не меняется при retry.
- Одновременно у session может быть только один незавершённый activation attempt.
- Одновременно у balance ID допускается не более одной новой reservation request от одной Telegram identity; глобальную wallet capacity контролирует внешний top-up.
- Reservation deadline задаёт top-up. Bot принимает диапазон от 1 до 60 минут; значение вне диапазона отклоняется как contract violation.
- `payment_confirmed` является claim, а не основанием активации.
- Active-sessions повторно проверяет transaction через top-up и активирует только terminal `VERIFIED_FINAL`.
- Проверяются network, asset, transaction ID, reservation ID, sender, receiver, exact amount и deadline policy.
- Partial, excess, wrong sender/receiver, duplicate transaction и post-expiry payment не активируют session; verdict формирует top-up, наша часть сохраняет отказ.
- Один `(network, transaction_id)` может активировать только одну session/operation.
- Technical retry выполняется с тем же operation ID; пользовательский повтор после terminal rejection создаёт новый operation ID.

## 7. Список и отзыв сессий

- List/revoke авторизует active-sessions по `requester_session_id`; `balance_id` из bot payload не принимается.
- Список сортируется: текущая сессия первой, затем `activated_at DESC, session_id`; pagination cursor обязателен, page size default 20, max 100.
- Revoke — soft transition `ACTIVE -> REVOKED`, повтор идемпотентен.
- Self-revoke разрешён только после явного Telegram confirmation. После success текущий dialog немедленно теряет active access.
- Revoke последней сессии разрешён. Новая binding создаётся новым invite; если invite относится к прежнему balance ID, активация требует тот же первоначальный sender wallet.
- Чужая session, несовпадающий balance identity или revoked requester дают безопасный отказ без раскрытия существования target.

## 8. Telegram transport modes

- `direct_polling` — local development и аварийный single-replica mode.
- `direct_webhook` — production default; несколько replicas за load balancer.
- `natsproxy` — production alternative, реализуется отдельным adapter по тем же application ports.
- Одновременно включён ровно один input mode. Startup проверяет и приводит Telegram webhook state к выбранному mode без `drop_pending_updates`.
- Webhook проверяет `X-Telegram-Bot-Api-Secret-Token`, body limit 256 KiB и request timeout 3 секунды.
- HTTP 2xx возвращается только после DB commit update dedup + domain transition/outbox. Внешних network calls внутри webhook transaction нет.
- Long-poll offset продвигается только после того же durable commit.
- `update_id` дедуплицируется 30 дней. Callback actions подписаны HMAC, одноразовые и живут 10 минут.

## 9. NATS и Ed25519

- Утверждён envelope/signature format v1 из [contracts.md](contracts.md).
- JSON envelope + base64 exact payload bytes; envelope unknown fields rejected, payload unknown fields ignored within v1.
- ID — UUIDv7; timestamps — UTC RFC3339Nano; message size limit — 256 KiB.
- Private key — PKCS#8 PEM; public key — PKIX PEM. В production читаются только из mounted secret files с permission check.
- Invite result, sender/receiver wallet и другие recoverable secrets шифруются AES-256-GCM. Ciphertext хранит key ID и random 96-bit nonce; AAD включает service, table, row ID и column. Lookup/compare выполняется по HMAC-SHA-256 fingerprint отдельным ключом.
- Encryption keyring допускает один active key и несколько decrypt-only keys. Rotation: добавить новый key, переключить active ID, re-encrypt batch worker, удалить старый только после метрики `rows_with_old_key=0` и backup retention window.
- Допустимый clock skew — ±2 минуты. Commands всегда имеют `expires_at`; events принимаются максимум 24 часа, если domain state ещё допускает переход.
- NATS transport использует TLS 1.3, NKEY/JWT credentials, отдельный account/subject ACL для каждого сервиса.
- Queue groups: `rw.bot.v1` и `rw.sessions.v1`; имена environment-prefixed в общей NATS infrastructure.
- No Request/Reply API, wildcard publish permission и dynamic reply subjects.
- `invite.created` имеет единственного разрешённого subscriber — principal внешнего issuer; другие сервисы не имеют ACL на этот subject.

## 10. Retry, inbox/outbox и reconciliation

- Outbox создаётся в одной транзакции с domain state.
- Retry schedule: 250 ms, multiplier 2, full jitter 20%, max interval 30 s.
- Commands повторяются с тем же `message_id` до terminal result или `expires_at`.
- Inbox хранит payload digest и result message ID. Same ID + different digest — security incident.
- Consumer повторно публикует сохранённый result для duplicate command.
- Outbox worker batch 100, max 8 concurrent publishes на replica; значения конфигурируемы, defaults одинаковы в dev/prod.
- Reconciliation запускается каждые 30 секунд, захватывает максимум 100 stale operations через `FOR UPDATE SKIP LOCKED`.
- После expiry операция переводится в explicit `EXPIRED` или `MANUAL_REVIEW`; бесконечных retries нет.
- Diagnostic outbox не может превышать 100000 записей или 1 GiB: после лимита одинаковые incidents агрегируются, основной flow не блокируется.

## 11. PostgreSQL и concurrency

- Transaction isolation по умолчанию `READ COMMITTED`; invariants обеспечиваются unique constraints, conditional updates и точечным `SELECT ... FOR UPDATE`.
- Network calls внутри DB transaction запрещены.
- Statement timeout 5 s, lock timeout 1 s, idle-in-transaction timeout 10 s.
- Runtime pool default: max 20, min 2 connections на replica; deployment обязан проверять общий budget БД.
- Outbox/reconciliation используют deterministic order + `FOR UPDATE SKIP LOCKED`.
- Migrations — forward-only в production, pattern expand/migrate/contract. Startup не запускает migrations.
- Pre-deploy migration Job берёт PostgreSQL advisory lock и проверяет текущую/целевую schema version.
- Каждая таблица имеет explicit CHECK/UNIQUE/FK, timestamps и необходимые partial indexes; soft-deleted rows сохраняют audit.

## 12. Retention

| Данные | Срок по умолчанию |
| --- | --- |
| Telegram update dedup | 30 дней |
| Expired/consumed invite technical rows | 30 дней после terminal state |
| Invite audit без token/digest | 180 дней |
| Junk inactive sessions | 7 дней без активности после expiry |
| Activation attempts/verifications | 365 дней |
| Revoked active sessions | 365 дней, затем anonymization identity fields |
| Inbox processed rows | 30 дней после максимального message expiry |
| Outbox confirmed rows | 30 дней; DEAD — 90 дней |
| Application logs | 30 дней |
| Security/audit logs | 180 дней |
| Metrics | 15 месяцев агрегированно |
| Traces | 7 дней, errors 30 дней |

Retention jobs работают малыми batch-ами, имеют rate limit и метрики. Юридическая политика может только увеличить/сократить сроки отдельным утверждённым override без изменения domain semantics.

## 12.1. Rate limits и защита от злоупотреблений

| Действие | Лимит v1 |
| --- | --- |
| Любые Telegram updates | 30 в минуту на Telegram identity, burst 10 |
| Неверный invite | 5 попыток за 10 минут, затем блокировка 30 минут |
| Создание activation reservation | 3 в час на Telegram identity и 10 в час на balance ID; одновременно только одна активная attempt |
| Список сессий | 30 запросов в минуту на requester session |
| Revoke | 10 запросов в час на requester session |
| Invite create по NATS | 100 commands/s на issuer principal на replica, burst 200; общий предел защищён DB pool/backpressure |

Security/business limits хранятся в PostgreSQL по HMAC fingerprint ключа субъекта и работают одинаково на всех replicas. Высокочастотный issuer ingress ограничивается локальным token bucket на каждой replica плюс NATS ACL. Превышение возвращает `RATE_LIMITED` с `retry_after`; существующая операция не отменяется.

## 13. Local и production delivery

- Local: Docker Compose, PostgreSQL x2, NATS без JetStream, оба сервиса, fake top-up, fake diagnostics, fake natsproxy/Telegram.
- Production: OCI images для `linux/amd64` и `linux/arm64`, non-root, read-only root filesystem, SBOM и vulnerability scan.
- Kubernetes deployment поставляется Helm chart; платформы без Kubernetes используют те же images/env/secret files.
- GitHub Actions: format, vet, staticcheck, govulncheck, unit/race, integration, contract, migration, image/SBOM/scan.
- Минимум 2 replicas каждого сервиса в production для `direct_webhook` и `natsproxy`; PodDisruptionBudget, anti-affinity и graceful drain 30 s. `direct_polling` — документированный аварийный single-replica mode и временно не выполняет HA SLO.
- PostgreSQL backup: daily full + continuous WAL/PITR; RPO 5 минут, RTO 60 минут. Restore rehearsal ежеквартально.

## 14. Наблюдаемость и SLO

- Availability durable command acceptance: 99.9% в месяц, исключая Telegram/top-up outage из нашей зоны ответственности.
- p95 обработки локального Telegram update до durable state: <500 ms; p99 <1.5 s.
- p95 invite result после получения NATS command: <1 s.
- p95 authoritative activation notification после verified event: <2 s.
- 99.9% terminal business messages подтверждены или попали в alert/manual review в пределах 5 минут после восстановления dependency.
- Alerts: readiness >5 min, outbox oldest >2 min warning/>10 min critical, DEAD messages >0, invalid signatures spike, slow consumer, DB pool >80%, lock wait, Telegram 429/5xx, reconciliation backlog.

## 15. Политика внешних контрактов

- Контракты в `contracts/asyncapi` и `contracts/schemas` принадлежат этому репозиторию для наших subjects.
- Команда-получатель владеет acceptance fixtures; обе стороны запускают один conformance bundle.
- Если существующий внешний сервис несовместим, создаётся explicit adapter или новый contract version; ослаблять подпись, идемпотентность и validation нельзя.
- До появления реального внешнего сервиса local fake является reference implementation только для согласованного wire behavior, но не для чужой бизнес-логики.

## 16. Статус вопросов

Технических вопросов, блокирующих начало разработки, нет. Значения credentials, production hostnames, bot token, public keys и конкретный `RW_USDT_NETWORK` являются deployment inputs, а не архитектурными вопросами; config validation не позволит запустить production без них.
