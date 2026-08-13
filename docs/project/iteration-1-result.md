# Результат первой итерации разработки

Статус: `ЗАВЕРШЕНО`  
Дата: `2026-08-13`  
Соответствует этапу 1 из [плана разработки](development-plan.md).

## Цель

Создать воспроизводимую основу для двух независимо развёртываемых сервисов до реализации бизнес-сценариев. На момент завершения итерация не включала invite, activation, list/revoke или Telegram transport. Позже поверх основы добавлен отдельный [демонстрационный direct-polling срез](telegram-demo.md); бизнес-сценарии по-прежнему зависят от следующих этапов.

## Реализовано

| Область | Результат |
| --- | --- |
| Go workspace | Module `github.com/dmuraveiko/RW`, Go/toolchain 1.26.5, фиксированные зависимости |
| Сервисы | Независимые команды `rw-bot` и `rw-active-sessions`, изолированные domain/app/adapter packages |
| Конфигурация | Однократный строгий разбор env, отказ при unknown `RW_*`, нарушении mode/secret/TLS/range policy |
| PostgreSQL | Отдельный `pgxpool`, server-major check, таймауты сессии и проверка версии схемы |
| NATS | Core NATS client без JetStream/Request-Reply, reconnect/slow-consumer callbacks и bounded buffer settings |
| Runtime | JSON-логи, graceful shutdown, `/health/live`, dependency-aware `/health/ready`, отдельный `/metrics` |
| Миграции | Отдельная non-runtime команда, PostgreSQL advisory session lock, независимые каталоги обеих БД |
| Криптография | Ed25519 PKCS#8/PKIX loading, trusted-key validity, AES-256-GCM keyring, AAD и HMAC-SHA-256 fingerprint |
| Контракты | Строгий envelope v1 codec/verifier, deterministic signing bytes, golden positive fixture, JSON Schemas и AsyncAPI |
| Поставка | Multi-stage digest-pinned image, non-root distroless runtime, Compose, Make targets и CI skeleton |

## Локальная демонстрация

```bash
make dev-up
curl --fail http://localhost:8081/health/ready
curl --fail http://localhost:8082/health/ready
curl --fail http://localhost:9091/metrics
```

Оба readiness endpoint должны вернуть `{"status":"ready"}`. Migration jobs завершаются с exit code 0 до старта сервисов. Контейнеры приложений работают от `nonroot:nonroot`.

## Выполненные проверки

- `gofmt`, `go vet`, unit tests, race tests и сборка всех команд;
- `staticcheck` и `govulncheck`; достижимых уязвимостей после обновления транзитивной зависимости нет;
- строгая конфигурация и fail-fast cases;
- golden Ed25519 vector и отказ при изменении payload;
- AES-GCM round trip и отказ при изменении AAD;
- сборка production-like контейнеров;
- миграция обеих чистых PostgreSQL 18.4;
- readiness обоих сервисов;
- восстановление NATS-соединения с увеличением reconnect metric;
- non-root runtime user.

## Следующая итерация

Этап 2 реализует transactional inbox/outbox, повтор команд и сохранённых результатов, reconciliation, bounded concurrency и contract fake peer. Доменные таблицы и сценарии не добавляются до прохождения crash matrix messaging substrate.
