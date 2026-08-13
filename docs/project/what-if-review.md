# Матрица «а что если…»

Документ предназначен для логического ревью техдиром. Колонка «решение» описывает ожидаемое поведение, а не скрывает вопрос инфраструктурой.

## Обмен сообщениями и порядок событий

| Что если | Риск | Решение/инвариант | Проверка |
| --- | --- | --- | --- |
| Подписчик был выключен при публикации | Команда потеряна Core NATS | Outbox повторяет ту же команду до итогового результата или истечения срока | Компонентный тест отказа |
| Публикация прошла, но сервис упал до отметки outbox | Повтор команды | Inbox получателя; повтор результата без повторного побочного эффекта | Тест аварийного завершения |
| Получатель выполнил commit в БД, но результат потерян | Отправитель продолжит повторы | Повтор команды вызывает повторную публикацию сохранённого результата | Тест аварийного завершения |
| Одно сообщение пришло дважды | Двойная активация или отзыв | Уникальный `message_id` в inbox + ограничения операции | Интеграционный тест |
| Один `message_id`, но другой payload | Атака или ошибка отправителя | Сравнить digest payload, отклонить, создать security incident | Негативный контрактный тест |
| Результат пришёл раньше ожидаемого локального состояния | Нарушение порядка | Сохранить или отклонить по state policy; запустить reconciliation, не форсировать переход | Тест машины состояний |
| Поздний платёж относится к старой попытке | Активация новой попытки чужой транзакцией | Проверить attempt, reservation, deadline и tx; старое состояние остаётся итоговым | E2E-тест позднего события |
| Несколько replicas получили событие | Повторная работа | Commands идут через queue group; идемпотентность БД всё равно обязательна | Тест нескольких replicas |
| Медленный получатель теряет сообщения | Невидимая рассинхронизация | Alert slow consumer + retry/reconciliation отправителя | Нагрузочный тест отказа |

## Invite и Telegram

| Что если | Решение/инвариант |
| --- | --- |
| Invite угадали/украли | 256-bit random, digest-only DB, TTL, single-use; это bearer secret до consumption |
| Два пользователя одновременно открыли invite | Atomic conditional consumption; активируется только одна inactive session |
| Пользователь прислал `/start` повторно | Показать текущее состояние существующей binding; не создавать новую |
| Пользователь начал без invite | Только безопасный manual invite flow |
| Telegram прислал update повторно | Durable dedup по `(transport, update_id)` |
| Webhook пришёл с неверным secret | 401/403 без domain processing; security metric |
| Telegram недоступен после успешной активации | Authority остаётся ACTIVE; notification retry, `/start` позже восстанавливает UI |
| Telegram принял `sendMessage`, но соединение оборвалось до ответа | Delivery становится `OUTCOME_UNKNOWN`; автоматического повтора нет, чтобы не дублировать сообщение; `/start` восстанавливает UI |
| Переключили polling ↔ webhook с pending updates | Deployment runbook, single active mode, dedup и явная `drop_pending_updates=false` policy |
| Пользователь заблокировал бота | Пометить delivery failure; active session автоматически не удалять |

## Активация и кошельки

| Что если | Решение/инвариант |
| --- | --- |
| Первый balance ID активируется конкурентно с двух Telegram accounts | Lock/unique balance identity; один wallet закрепляется, одинаковый wallet допускает обе сессии по policy |
| Два первых запроса имеют разные wallets | Только один commit; второй получает `WALLET_MISMATCH` |
| Top-up сначала подтвердил, затем опроверг | Active-sessions принимает только финальный verified contract; политика post-finality reversal должна прийти от top-up owner |
| Сумма/receiver/sender/tx не совпали | Verification rejected, сессия не активируется, diagnostic по типу mismatch |
| Платёж частичный или превышает сумму | Наша часть активирует только при exact `VERIFIED_FINAL`; любой другой verdict сохраняется как отказ |
| Одинаковый tx использован для двух сессий | Unique transaction identity; второй flow rejected |
| Wallet формат зависит от сети | Не нормализовать эвристически; network-specific validator из утверждённого contract |
| Повторная активация после смены владельцем wallet | По текущему REQ запрещена; нужен отдельный будущий recovery/change policy |

## Управление сессиями

| Что если | Решение/вопрос |
| --- | --- |
| Пользователь отзывает уже revoked session | Идемпотентный success с существующим `revoked_at` либо deterministic already-revoked result |
| Пользователь пытается удалить чужую session | Balance identity выводится из requester; `NOT_AUTHORIZED` |
| Отзывается текущая session | Разрешить после явного подтверждения; после authoritative result текущий dialog немедленно теряет active access |
| Отзывается последняя session | Разрешить после confirmation; новая binding требует invite, а для прежнего balance ID — repeat activation тем же wallet |
| Bot projection отстаёт | Authoritative list/revoke идёт в active-sessions; projection не используется для destructive authorization |
| Revoke event потерян для bot | Outbox retry; projection reconciliation/snapshot |

## Инфраструктура и данные

| Что если | Решение/инвариант |
| --- | --- |
| Bot DB недоступна | Readiness false; no mutation accepted; не продолжать flow в памяти |
| Sessions DB недоступна | Verification/list/revoke pending; bot не подменяет authority |
| NATS недоступен несколько часов | Outbox растёт в пределах capacity; alerts; expiry/reconciliation после восстановления |
| Diagnostics недоступна | Не блокировать основной flow; bounded diagnostic outbox/aggregation |
| Сервис рестартовал после любой транзакции | Durable state + workers продолжают с последнего committed state |
| Backup восстановили на старый момент | Replay/reconciliation; inbox/outbox audit; projection rebuild; внешний tx уникален |
| Миграция не совместима со старым pod | Запрещено: expand/contract и pre-deploy compatibility test |
| Резко вырос трафик/спам | Per-user/balance/issuer rate limits, queue groups, bounded pools, cleanup и capacity alerts |
| Outbox бесконечно не доставляется | Max age/attempt policy, DEAD state, alert и ручной replay tool с аудитом |

## Безопасность

| Что если | Решение/инвариант |
| --- | --- |
| Ed25519 private key скомпрометирован | Emergency revoke key ID, rotate trusted manifests, pause producer subjects, audit replay window |
| Старое валидно подписанное сообщение повторили | `message_id` inbox + expiry + producer/key allowlist |
| NATS account украден, но signing key нет | Подделка не проходит application signature |
| Signing key есть, но NATS ACL нет | Publish запрещён transport layer; оба слоя обязательны |
| В error попал invite/wallet/token | Central redaction + structured error codes + snapshot security tests |
| Огромный payload/JSON bomb | Application size/depth limits до schema/domain processing |

## Не закрытые логические вопросы для техдира

1. Что является Telegram session identity и может ли один user иметь несколько balance IDs?
2. Разрешены ли self-revoke и удаление последней сессии?
3. Какой verdict top-up считается окончательным и может ли он быть отозван?
4. Каков бизнес-SLA активации и сколько времени повторять commands?
5. Как ведём себя при успешном late payment после expiry?
6. Какой retention нужен для activation/transaction audit и revoked sessions?
7. Кто владелец общей NATS subject/signature specification и key registry?
