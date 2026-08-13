# Демонстрационный Telegram-срез

Статус: `ДОСТУПНО ДЛЯ ЛОКАЛЬНОЙ ДЕМОНСТРАЦИИ`  
Дата: `2026-08-13`

Срез подтверждает прямое взаимодействие `rw-bot` с Telegram Bot API по требованиям `raw/rw_cab0.txt:35-49`. Он не заменяет этапы реализации invite и activation.

## Возможности

- проверка токена и identity через `getMe`;
- переход из webhook в polling без удаления накопленных updates;
- long polling с отменой при graceful shutdown и bounded retry backoff;
- обработка только message updates;
- durable dedup по Telegram `update_id` в Bot PostgreSQL;
- `/start`, `/help`, `/status` и безопасный ответ на неизвестную команду;
- полное игнорирование group/channel updates без создания сессии;
- запись `OUTCOME_UNKNOWN` без автоматического повтора `sendMessage`.

## Запуск

```bash
RW_TELEGRAM_TOKEN='<token from BotFather>' make dev-up-telegram
curl --fail http://localhost:8081/health/ready
```

После успешного `getMe` и запуска polling readiness становится доступен. Bot ID и username в local polling обнаруживаются автоматически; в staging/prod они остаются обязательными и сверяются с token identity.

## Ограничения среза

- `/start <invite>` не сохраняет и не потребляет invite;
- состояние binding/activation отсутствует;
- нет inline keyboard, list/revoke и webhook;
- polling рассчитан на одну локальную реплику.
- retention cleanup для `telegram_updates` будет добавлен вместе с полным Telegram transport; демонстрационная таблица пока не очищается автоматически.

Пользовательские ответы прямо сообщают об этих ограничениях и не изображают успешную привязку или активацию.
