# Текущее состояние реализации

Дата сверки: `2026-08-14`
Статус: `ПОЛНЫЙ TELEGRAM ACTIVATION VERTICAL SLICE`

Документ сопоставляет фактический код с исходным `raw/rw_cab0.txt`. Он не меняет scope и архитектурные решения.

## Что уже работает

| Область | Состояние | Основание |
| --- | --- | --- |
| Два Go-сервиса | Оба собираются и запускаются независимо | Решение архитектуры; подготовка к `raw/rw_cab0.txt:35-90` |
| PostgreSQL | Две отдельные БД, baseline migrations, version check | `raw/rw_cab0.txt:51`, `raw/rw_cab0.txt:82` |
| Core NATS | Подключение, queue consumers, encrypted inbox/outbox delivery, retry и activation reconciliation | `raw/rw_cab0.txt:39-40`, `raw/rw_cab0.txt:79-80` |
| Ed25519 | Envelope codec, signing/verifying, golden fixture и реальные activation/top-up messages | `raw/rw_cab0.txt:48-49`, `raw/rw_cab0.txt:79-80` |
| Active-sessions activation | Независимая top-up verification, атомарный first sender wallet, repeat activation и deterministic rejection | `raw/rw_cab0.txt:77-90` |
| Invite и binding | Signed NATS create/result, single-use consume и отдельная inactive session | `raw/rw_cab0.txt:3-13`, `raw/rw_cab0.txt:53-55` |
| Telegram activation | Polling, sender wallet dialog, reserve/payment/verify orchestration и active projection | `raw/rw_cab0.txt:15-18`, `raw/rw_cab0.txt:35-65` |
| Эксплуатационный каркас | Strict config, JSON logging, health/readiness, metrics, graceful shutdown, non-root image | Design decision для надёжного запуска |

## Что реализовано частично

| Область | Уже есть | Ещё требуется |
| --- | --- | --- |
| Telegram transport | `getMe`, `deleteWebhook`, `getUpdates`, `sendMessage`, polling backoff | Webhook, full delivery state, rate-limit policy, callbacks, natsproxy parity |
| NATS contracts | Subject catalog, envelope, AsyncAPI, invite/reserve/activation schemas и реальные handlers | Завершить schemas/fixtures для list/revoke/natsproxy |
| Messaging reliability | Inbox/outbox, encrypted envelopes, retry, duplicate result и top-up reconciliation | Полная crash matrix, retention workers, lag/dead metrics и все business flows |
| Active-sessions | Activation verification и authoritative sender-wallet invariant | List/revoke, snapshot, production top-up conformance и полная failure matrix |
| Telegram update storage | Durable dedup и outcome | Retention worker, recovery metrics и остальные transport modes |

## Что по исходной задаче ещё не реализовано

- список и отзыв сессий через NATS — `raw/rw_cab0.txt:22-25`, `raw/rw_cab0.txt:65`;
- natsproxy и diagnostics adapters — `raw/rw_cab0.txt:39`, `raw/rw_cab0.txt:67`.

Функции receiver wallet, пополнения пользовательского баланса и делегирования остаются вне текущего scope согласно `raw/rw_cab0.txt:26-29`, `raw/rw_cab0.txt:45-46`, `raw/rw_cab0.txt:87`.

## Правильный следующий порядок

1. Закрыть active-sessions и Telegram UX: list/revoke и snapshot.
2. Добавить rate limits и durable Telegram delivery queue.
3. Завершить messaging retention/metrics и production top-up conformance.
4. Расширить concurrency/crash/failure matrix.
5. Полный Telegram UX, webhook, затем natsproxy/diagnostics и production hardening.

Activation flow логически завершён и воспроизводится end-to-end. Telegram UI не является источником истины: локальная active projection появляется только после авторитетного результата active-sessions.
