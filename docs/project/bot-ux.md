# Telegram UX первой версии

Статус: `УТВЕРЖДЕНО ДЛЯ V1`  
Язык: русский. Тексты находятся в presentation catalog и не зашиваются в domain/use-case packages.

## Команды

| Команда/действие | Доступ | Результат |
| --- | --- | --- |
| `/start <invite>` | Любая Telegram identity | Проверка invite и создание/восстановление binding |
| `/start` | Любая | Показ текущего состояния; без binding — запрос invite |
| `/help` | Любая | Краткая справка, соответствующая текущему access level |
| «Ввести invite» | Unbound | Перевод dialog в ожидание token |
| «Активировать» | Bound inactive/revoked | Запрос sender wallet |
| «Проверить статус» | Pending activation | Показ локального durable state; запускает reconciliation hint, но не новый top-up |
| «Отменить ввод» | До публикации reservation command | Возврат в inactive menu без внешней операции |
| «Мои сессии» | Active | Асинхронный authoritative list через active-sessions |
| «Отозвать» | Active | Выбор target → confirmation → revoke command |
| «Отмена» | В любом dialog step | Возврат в безопасное меню без отмены уже подтверждённой внешней операции |

Неизвестные slash-команды дают `/help`, не меняя state. Updates из group/channel полностью игнорируются: бот не отвечает и не обрабатывает invite/wallet.

## Сценарий без invite

1. `/start` сообщает, что для привязки нужен invite.
2. Кнопка «Ввести invite» переводит в `AwaitingInviteInput` на 10 минут.
3. Следующее text message трактуется только как invite token, trims surrounding whitespace, затем raw text немедленно удаляется из application context после digest lookup.
4. Ошибки unknown/expired/consumed имеют один текст: «Инвайт недействителен или срок его действия истёк».
5. После 5 неверных попыток пользователь получает retry time без раскрытия rate-limit internals.

## Ввод sender wallet

- Bot явно показывает asset и configured network до ввода.
- Сообщение принимается только в ожидающем state, trim-ится и проходит strict network validator.
- В чат не отправляется полный wallet повторно: confirmation показывает первые/последние безопасные символы.
- Перед reservation пользователь подтверждает: network, masked sender wallet и то, что перевод должен уйти именно с этого wallet.
- Другой текст/attachment отклоняется без state mutation.

## Ожидание платежа

Показываются:

- asset/network;
- exact amount как decimal string;
- receiver wallet в copy-friendly code formatting;
- абсолютный deadline в UTC и локализованное оставшееся время;
- предупреждение не отправлять после expiry;
- кнопка «Проверить статус»; после выдачи reservation локальная отмена отсутствует, attempt завершается результатом или expiry.

Bot не обещает activation по предварительному claim. После платежа текст: «Платёж найден, выполняется окончательная проверка».

## Список и отзыв

- Page size 20; «Назад/Далее» используют signed one-time callback tokens.
- Элемент: sanitized label или `Telegram ••••1234`, короткий session ID, дата активации, `Текущая` marker.
- Для каждого active target есть «Отозвать».
- Confirmation называет target и отдельно предупреждает при self/last-session revoke.
- После self-revoke bot очищает active menu и сообщает, что для новой binding потребуется новый invite.

## Отображение ошибок

| Domain code | Пользовательское поведение |
| --- | --- |
| `INVALID_ARGUMENT` | Подсказка корректного формата без echo секрета |
| `WALLET_MISMATCH` | Сообщить, что нужен wallet первой активации; не показывать сохранённый адрес |
| `EXPIRED` | Предложить начать новую attempt/invite operation |
| `RATE_LIMITED` | Показать точное безопасно округлённое время следующей попытки |
| `TEMPORARILY_UNAVAILABLE` | Сохранить state, показать retry time/«Проверить статус» |
| `NOT_AUTHORIZED/NOT_FOUND` | Единый нейтральный текст для session operations |
| `CONTRACT_VIOLATION/INTERNAL` | Нейтральная ошибка + короткий support correlation code |

Пользователю никогда не показываются stack trace, NATS subject, DB error, external payload или internal ID целиком.

## Повторные updates и notifications

- Duplicate Telegram update возвращает уже рассчитанное presentation outcome или no-op.
- Callback token single-use, HMAC-signed, bound to session/action/target и живёт 10 минут.
- Для `sendMessage` outcome unknown не повторяется автоматически; актуальное состояние доступно через `/start`.
- `editMessageText` используется для progress там, где известен message ID; `message is not modified` считается success.

## Accessibility и локализация

- Смысл не кодируется только emoji или цветом.
- Wallet/amount доступны обычным текстом для copy/paste.
- Callback labels короткие; длинные explanations отправляются отдельным сообщением.
- Catalog key является стабильным; добавление другого языка не меняет domain state machine.
