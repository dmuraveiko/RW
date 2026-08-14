# ADR-010. Полные ожидаемые факты проверки активации

Статус: `ПРИНЯТО`

Дата: `2026-08-13`

## Контекст

Active-sessions обязан независимо перепроверить подтверждение top-up и сравнить проверенные факты с заявленными до создания активной сессии. Ранее описанный payload `rw.sessions.v1.activation.verify` не содержал `receiver_wallet`, `amount`, `network`, `offer_expires_at` и `bot_id`, поэтому consumer не мог выполнить все утверждённые проверки и сохранить Telegram identity без неявного внешнего состояния.

## Решение

Команда `rw.sessions.v1.activation.verify` обязательно передаёт:

- identity: `operation_id`, `session_id`, `balance_id`, `bot_id`, `telegram_user_id`, `telegram_chat_id`;
- ожидаемые факты платежа: `sender_wallet`, `receiver_wallet`, `amount`, `asset=USDT`, `network`, `transaction_id`, `external_reservation_id`;
- временное окно `offer_valid_from`–`offer_expires_at`;
- необязательный безопасный `display_label`.

Active-sessions пересылает ожидаемые факты в `rw.topup.v1.activation.verify`, принимает нормализованный результат top-up, сравнивает все значения и только после этого применяет invariant первоначального sender wallet.

## Совместимость

Subject и major version не меняются: поля добавлены до появления production consumer и не переиспользуют существующую семантику. JSON envelope остаётся совместимым. Старый неполный producer не может безопасно активировать сессию и получает `INVALID_ARGUMENT`; все команды должны проходить обновлённую consumer-owned schema до интеграции.

## Последствия

- Active-sessions не зависит от скрытого состояния бота при проверке платежа.
- Повторная проверка детерминирована и восстанавливается после рестарта.
- В payload присутствуют чувствительные identifiers; транспорт обязан использовать утверждённые TLS/ACL/Ed25519 policies, а логи не должны выводить payload.
