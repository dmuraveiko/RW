# Рабочая документация RealWallet CAB0

Статус: `ТЕХНИЧЕСКАЯ СПЕЦИФИКАЦИЯ УТВЕРЖДЕНА`  
Версия: `1.4`
Дата: `2026-08-14`

Этот пакет описывает подготовку к реализации только двух подсистем из `raw/rw_cab0.txt`: Telegram-бота и активных сессий. Подсистемы пополнения, blockchain-статусов, `natsproxy` и диагностики считаются внешними.

## Как читать

1. [Объём работ, допущения и решения](scope-and-decisions.md)
2. [Утверждённые технические решения](technical-decisions.md)
3. [Архитектура](architecture.md)
4. [Сценарии и машины состояний](workflows.md)
5. [Контракты NATS и Ed25519](contracts.md)
6. [Модель PostgreSQL](data-model.md)
7. [Конфигурация сервисов](configuration.md)
8. [Telegram UX первой версии](bot-ux.md)
9. [Надёжность и масштабирование](reliability-and-scalability.md)
10. [Безопасность и эксплуатация](security-and-operations.md)
11. [Стратегия тестирования](testing-strategy.md)
12. [Пошаговый план разработки](development-plan.md)
13. [Матрица «а что если…»](what-if-review.md)
14. [Матрица трассировки требований](traceability.md)
15. [Реестр закрытых технических вопросов](open-questions.md)
16. [Чек-лист архитектурного ревью](review-checklist.md)
17. [Результат первой итерации](iteration-1-result.md)
18. [Демонстрационный Telegram-срез](telegram-demo.md)
19. [Текущее состояние реализации](current-status.md)
20. [ADR-010: полные факты проверки активации](adr/010-activation-verification-facts.md)
21. [Интеграция с activation verification](integration-active-sessions.md)
22. [Результат итерации II-A](iteration-2a-result.md)
23. [Результат итерации II-B](iteration-2b-result.md)

## Статус реализации

Этап 1 завершён. Реализованы демонстрируемые срезы этапов 2–6: PostgreSQL inbox/outbox, signed invite/reserve/activation messages, authoritative active-sessions activation, single-use invite, inactive binding и полный Telegram polling flow до статуса `ACTIVE`. Следующая прикладная вертикаль — list/revoke сессий.

## Статусы утверждений

- **REQ / ТРЕБОВАНИЕ** — прямое требование `rw_cab0.txt`; менять можно только после изменения источника или явного решения заказчика.
- **DECISION / ПРИНЯТО** — утверждённое техническое решение, обязательное для реализации.
- **ASSUMPTION / ДОПУЩЕНИЕ** — временное допущение; код не должен делать его необратимым.
- **EXTERNAL-CONTRACT / ВНЕШНИЙ КОНТРАКТ** — ожидание от команды внешнего сервиса; должно быть согласовано контрактными тестами.
- **OUT-OF-SCOPE / ВНЕ ОБЪЁМА** — намеренно не реализуется в этом репозитории.

## Критерий готовности документации

Техническая часть готова к реализации: P0–P2 закрыты, формат контрактов и критерии приёмки утверждены. Перед интеграцией владельцы внешних сервисов должны запустить общий conformance bundle; это не блокирует разработку против reference fakes.
