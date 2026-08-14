# RealWallet CAB0

Монорепозиторий двух Go-сервисов: Telegram-бота `rw-bot` и авторитетного сервиса активных сессий `rw-active-sessions`. Текущий объём работ определён в [рабочей документации](docs/project/index.md).

## Локальный запуск

Требуется Docker с Compose v2.

```bash
make dev-up
curl --fail http://localhost:8081/health/ready
curl --fail http://localhost:8082/health/ready
```

Метрики доступны на `http://localhost:9091/metrics` и `http://localhost:9092/metrics`. Логи двух сервисов: `make dev-logs`. Остановка без удаления данных: `make dev-down`.

Ключи и пароли в `deploy/compose` предназначены только для локального стенда и не принимаются production-конфигурацией.

## Проверки

Проект использует Go 1.26.5.

```bash
make check
make test-race
```

Сервисы не применяют миграции при запуске. В Compose миграции выполняются отдельными одноразовыми jobs до старта приложений.

## Демонстрация active-sessions

После `make dev-up` можно выполнить воспроизводимый сценарий проверки активации:

```bash
make demo-active-sessions
```

Сценарий показывает первую и повторную активацию, идемпотентную обработку duplicate command, отказ `WALLET_MISMATCH` и защиту от позднего результата top-up. Для взаимодействия используются подписанные асинхронные NATS-сообщения и локальный fake top-up. Подробности и ссылки на контракты находятся в [инструкции интеграции](docs/project/integration-active-sessions.md).

## Telegram-бот в режиме polling

1. Откройте `@BotFather` в Telegram.
2. Выполните `/newbot` и скопируйте выданный HTTP API token.
3. Подготовьте локальную конфигурацию и вставьте token в `.env`:

```bash
cp .env.example .env
# отредактируйте RW_TELEGRAM_TOKEN в .env
make dev-up-telegram
```

Либо передайте token только для одного запуска:

```bash
RW_TELEGRAM_TOKEN='123456789:replace-with-token-from-botfather' make dev-up-telegram
```

`.env` уже создан локально и исключён из Git; в репозиторий попадает только безопасный `.env.example`. После запуска откройте созданного бота и отправьте `/start`. В демонстрационном срезе доступны `/start`, `/help` и `/status`.

Локальный polling-стенд запускает reference fake top-up: он автоматически подтверждает тестовую проверку, реальные средства отправлять не нужно. Создайте одноразовый invite:

```bash
RW_BALANCE_ID='local-user-1' make create-invite
```

Команда выведет `bot_deep_link`. Откройте ссылку, отправьте тестовый sender wallet и дождитесь сообщения об успешной активации. Для проверки без Telegram network доступен воспроизводимый сценарий:

```bash
make dev-up
make demo-telegram-flow
```

Проверка запуска и логи:

```bash
curl --fail http://localhost:8081/health/ready
make dev-logs
```

Polling предназначен для локальной разработки и запускается в одной реплике. Токен, invite, balance ID и полные кошельки не выводятся в логи. Для остановки используйте `make dev-down`.
