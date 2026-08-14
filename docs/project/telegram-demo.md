# Демонстрационный Telegram-срез

Статус: `ПОЛНЫЙ ACTIVATION FLOW ДОСТУПЕН ДЛЯ ЛОКАЛЬНОЙ ДЕМОНСТРАЦИИ`

Дата: `2026-08-14`

Срез подтверждает прямое взаимодействие `rw-bot` с Telegram Bot API и полный сценарий активации по требованиям `raw/rw_cab0.txt:3-18`, `raw/rw_cab0.txt:35-65`.

## Возможности

- проверка токена и identity через `getMe`;
- переход из webhook в polling без удаления накопленных updates;
- long polling с отменой при graceful shutdown и bounded retry backoff;
- обработка только message updates;
- durable dedup по Telegram `update_id` в Bot PostgreSQL;
- создание одноразового invite через подписанный NATS contract;
- `/start <invite>` и ручной ввод invite;
- отдельная inactive session и persistent activation attempt;
- ввод sender wallet, reservation, payment claim и authoritative verification;
- перенос в active projection только после ответа `rw-active-sessions`;
- `/start`, `/help`, `/status` и безопасный ответ на неизвестную команду;
- полное игнорирование group/channel updates без создания сессии;
- запись `OUTCOME_UNKNOWN` без автоматического повтора `sendMessage`.

## Запуск

```bash
RW_TELEGRAM_TOKEN='<token from BotFather>' make dev-up-telegram
curl --fail http://localhost:8081/health/ready
RW_BALANCE_ID='local-user-1' make create-invite
```

После успешного `getMe` и запуска polling readiness становится доступен. Bot ID и username в local polling обнаруживаются автоматически; в staging/prod они остаются обязательными и сверяются с token identity.

Reference fake top-up автоматически завершает локальную проверку. Реальные USDT в этом режиме отправлять не нужно.

## Оставшиеся ограничения

- нет inline keyboard, list/revoke и webhook;
- polling рассчитан на одну локальную реплику.
- rate limits, durable Telegram delivery queue и retention cleanup остаются production hardening;
- production top-up должен пройти conformance tests вместо reference fake.

Авторитетным источником активации остаётся `rw-active-sessions`; событие top-up само по себе не переводит локальную сессию в active.
