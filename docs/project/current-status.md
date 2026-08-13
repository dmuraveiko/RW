# Текущее состояние реализации

Дата сверки: `2026-08-13`  
Статус: `ПЛАТФОРМЕННАЯ ОСНОВА + LOCAL TELEGRAM DEMO`

Документ сопоставляет фактический код с исходным `raw/rw_cab0.txt`. Он не меняет scope и архитектурные решения.

## Что уже работает

| Область | Состояние | Основание |
| --- | --- | --- |
| Два Go-сервиса | Оба собираются и запускаются независимо | Решение архитектуры; подготовка к `raw/rw_cab0.txt:35-90` |
| PostgreSQL | Две отдельные БД, baseline migrations, version check | `raw/rw_cab0.txt:51`, `raw/rw_cab0.txt:82` |
| Core NATS | Подключение, TLS/auth config, reconnect lifecycle; business consumers ещё отсутствуют | `raw/rw_cab0.txt:39-40`, `raw/rw_cab0.txt:79-80` |
| Ed25519 | Envelope codec, signing/verifying и golden fixture реализованы; business messages ещё не отправляются | `raw/rw_cab0.txt:48-49`, `raw/rw_cab0.txt:79-80` |
| Telegram direct HTTP | Local single-replica polling, identity check, update dedup и три demo-команды | `raw/rw_cab0.txt:35-39` |
| Эксплуатационный каркас | Strict config, JSON logging, health/readiness, metrics, graceful shutdown, non-root image | Design decision для надёжного запуска |

## Что реализовано частично

| Область | Уже есть | Ещё требуется |
| --- | --- | --- |
| Telegram transport | `getMe`, `deleteWebhook`, `getUpdates`, `sendMessage`, polling backoff | Webhook, full delivery state, rate-limit policy, callbacks, natsproxy parity |
| NATS contracts | Subject catalog, envelope, AsyncAPI и начальные JSON Schemas | Полные payload schemas, contract harness, inbox/outbox и реальные handlers |
| Telegram update storage | Durable dedup и outcome | Retention worker, recovery metrics и остальные transport modes |

## Что по исходной задаче ещё не реализовано

- генерация и однократное потребление invite — `raw/rw_cab0.txt:3-11`, `raw/rw_cab0.txt:53-55`;
- inactive binding и ограничение доступного функционала — `raw/rw_cab0.txt:10-13`, `raw/rw_cab0.txt:69-71`;
- первичная и повторная активация через USDT — `raw/rw_cab0.txt:15-18`, `raw/rw_cab0.txt:42`, `raw/rw_cab0.txt:57-63`;
- авторитетная повторная проверка active-sessions — `raw/rw_cab0.txt:77-90`;
- список и отзыв сессий через NATS — `raw/rw_cab0.txt:22-25`, `raw/rw_cab0.txt:65`;
- natsproxy и diagnostics adapters — `raw/rw_cab0.txt:39`, `raw/rw_cab0.txt:67`.

Функции receiver wallet, пополнения пользовательского баланса и делегирования остаются вне текущего scope согласно `raw/rw_cab0.txt:26-29`, `raw/rw_cab0.txt:45-46`, `raw/rw_cab0.txt:87`.

## Правильный следующий порядок

1. Надёжный NATS substrate: inbox/outbox, retries, duplicate result и reconciliation.
2. Авторитетный active-sessions domain: activation verification, первый sender wallet, list/revoke.
3. Invite и inactive-session domain бота.
4. Оркестрация activation между bot, active-sessions и fake top-up.
5. Полный Telegram UX, webhook, затем natsproxy/diagnostics и production hardening.

Это сохраняет исходные границы и не делает Telegram UI источником истины. Текущий polling demo показывает транспортный прогресс, но не считается реализацией продуктового сценария.
