# Матрица трассировки требований

| Требование | Источник | Владелец/компонент | Хранение | Контракт | Приёмочная проверка |
| --- | --- | --- | --- | --- | --- |
| Создать invite по balance ID через NATS | `raw/rw_cab0.txt:53-55` | rw-bot invite use case | `invites`, inbox/outbox | `invite.create/created` | Idempotent create component test |
| Запуск через deep link | `raw/rw_cab0.txt:7-8` | Telegram adapter + binding use case | `telegram_updates`, `invites`, `inactive_sessions` | Telegram update | Polling/webhook E2E |
| Ручной ввод invite | `raw/rw_cab0.txt:7-8` | Bot dialog state machine | `inactive_sessions` | Telegram messages | Manual-flow E2E |
| Однократная привязка Telegram-сессии | `raw/rw_cab0.txt:10-11` | Bot binding policy | DB unique/conditional update | — | Concurrent binding test |
| Несколько сессий на balance ID | `raw/rw_cab0.txt:10-11` | Active-sessions | `balance_identities`, `active_sessions` | activation/list | Two-session E2E |
| Inactive доступна только активация | `raw/rw_cab0.txt:13` | Bot authorization gate | inactive state/projection | — | Command authorization unit/E2E |
| Bot генерирует verification amount | `raw/rw_cab0.txt:57-60` | Bot activation policy | `activation_attempts` | `activation.reserve` | Exact decimal contract test |
| Получить receiver/time window или retry later | `raw/rw_cab0.txt:60` | Top-up adapter | activation attempt | reserve results | Fake top-up component tests |
| Получить успешный top-up и tx ID | `raw/rw_cab0.txt:61-63` | Bot orchestration | activation attempt | `payment_confirmed` | Duplicate/late event tests |
| Повторно проверить активацию | `raw/rw_cab0.txt:89-90` | Active-sessions | `activation_verifications` | top-up verify/result | False claim rejection E2E |
| Сохранить первый sender wallet | `raw/rw_cab0.txt:15-18`, `raw/rw_cab0.txt:84-85` | Active-sessions domain | `balance_identities` | activation result | Concurrent same/different wallet tests |
| Разделить inactive/active storage | `raw/rw_cab0.txt:71` | Bot + sessions data ownership | Separate tables/DBs | Authoritative activation event | Schema review + E2E |
| Запросить список сессий по NATS | `raw/rw_cab0.txt:65` | Active-sessions list use case | `active_sessions` | `session.list/listed` | Authorization/pagination tests |
| Удалить сессию по NATS | `raw/rw_cab0.txt:65` | Active-sessions revoke use case | soft revoke fields | revoke results | Idempotent/foreign-target tests |
| Telegram direct HTTP | `raw/rw_cab0.txt:37-40` | Direct Telegram adapters | update dedup | Bot API HTTP | Polling + webhook contract/E2E |
| Telegram через natsproxy | `raw/rw_cab0.txt:39` | Адаптер natsproxy | Дедупликация updates | `telegram.update_received/call_*` v1 | Conformance fixtures + проверка одинакового поведения |
| Async NATS без JetStream/Request-Reply | `raw/rw_cab0.txt:40` | Messaging platform | inbox/outbox | All NATS flows | Static architecture check + failure suite |
| Ed25519 signing/verification | `raw/rw_cab0.txt:48-49`, `raw/rw_cab0.txt:79-80` | Crypto/envelope adapter | key config + inbox audit | Envelope v1 | Cross-language vectors/negative tests |
| PostgreSQL | `raw/rw_cab0.txt:51`, `raw/rw_cab0.txt:82` | Both services | All domain state | — | Migration/integration/restore tests |
| Диагностировать сбои | `raw/rw_cab0.txt:67` | Адаптер diagnostics | Ограниченный outbox/агрегация | `incident.report` | Тесты отказа зависимости |

Строка считается закрытой только тогда, когда существуют реализация, автоматическая приёмочная проверка и ссылка на актуальные источник и контракт.
