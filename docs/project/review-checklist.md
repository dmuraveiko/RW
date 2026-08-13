# Чек-лист архитектурного ревью

## Объём работ

- [x] Каждый реализуемый use case трассируется в `rw_cab0.txt`.
- [x] `rw_topup`/`rw_status` не реализуются внутри наших сервисов.
- [x] Extension seams не превращены в преждевременные таблицы/UI.

## Domain logic

- [x] Определено понятие Telegram session.
- [x] Утверждены invite TTL/reuse и activation expiry policies.
- [x] Первый sender wallet invariant имеет точную область действия.
- [x] Утверждены self-revoke/last-session правила.
- [x] Late/duplicate/out-of-order transitions определены.

## Contracts

- [x] Subjects, schemas, error taxonomy и owners определены нашей v1 specification.
- [x] Ed25519 signature input и формат test vectors однозначны между языками.
- [x] Expiry, replay, idempotency и retry semantics документированы.
- [ ] Top-up, natsproxy и diagnostics contract tests доступны в CI.
- [x] Есть versioning/rollout strategy для breaking change.

## Data

- [x] Для каждой таблицы назначен единственный owner.
- [ ] Constraints физически защищают invariants.
- [x] Нет cross-service SQL access или distributed transaction.
- [x] Retention/cleanup/privacy policy утверждены.
- [ ] Backup/restore и projection rebuild проверяемы.

## Надёжность и масштабирование

- [x] Core NATS message loss компенсирован business retry/reconciliation в design.
- [ ] Inbox/outbox crash matrix проходит.
- [x] Queue groups, worker pools, DB pools и rate limits имеют стартовые значения.
- [ ] Slow consumer/reconnect/outbox lag наблюдаемы.
- [ ] External outage не приводит к ложной активации.

## Безопасность

- [ ] NATS TLS/auth/ACL и application signatures используются совместно.
- [ ] Secrets приходят из утверждённого secret store.
- [ ] Key rotation/revocation runbook проверен.
- [ ] Invite/PII/wallet не попадают в telemetry.
- [ ] List/revoke authorization выполняется authoritative service.

## Поставка и эксплуатация

- [x] Local quickstart работает без скрытых ручных шагов.
- [ ] Images non-root/immutable и имеют SBOM/vulnerability scan.
- [ ] Migrations совместимы с rolling deployment и rollback plan.
- [x] Health/readiness semantics отражают реальную готовность текущего этапа.
- [ ] SLO, dashboards, alerts и runbooks согласованы.
- [ ] Staging failure/load/restore rehearsal завершён.

## Утверждение

| Роль | Имя | Решение/дата |
| --- | --- | --- |
| Технический директор | Не назначен в репозитории | |
| Владелец Telegram bot | Команда этого репозитория | Техническая спецификация v1.0 принята 2026-08-10 |
| Владелец active-sessions | Команда этого репозитория | Техническая спецификация v1.0 принята 2026-08-10 |
| Владелец top-up contract | Не назначен в репозитории | |
| Владелец natsproxy contract | Не назначен в репозитории | |
| Эксплуатация/SRE | Не назначен в репозитории | |
