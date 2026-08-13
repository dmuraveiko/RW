# Сценарии и машины состояний

## Создание invite

```mermaid
sequenceDiagram
    participant E as External initiator
    participant N as Core NATS
    participant B as rw-bot
    participant DB as Bot DB
    E->>N: invite.create(message_id, balance_id)
    N->>B: invite.create
    B->>DB: inbox dedup + invite + outbox (one transaction)
    B->>N: invite.created(correlation_id, invite URL)
    Note over E,B: При повторе create с тем же message_id возвращается тот же сохранённый результат
```

## Привязка и активация

```mermaid
sequenceDiagram
    participant U as Telegram user
    participant B as rw-bot
    participant DB as Bot DB
    participant T as External top-up
    participant S as rw-active-sessions
    participant SD as Sessions DB

    U->>B: /start invite
    B->>DB: consume invite + create inactive session
    B-->>U: запрос sender wallet
    U->>B: sender wallet
    B->>DB: create activation attempt + outbox
    B-->>T: activation.reserve
    T-->>B: activation.reserved / retry_later
    B-->>U: receiver, amount, deadline
    T-->>B: payment.confirmed(tx_id)
    B->>DB: save claim + outbox activation.verify
    B-->>S: activation.verify
    S->>SD: create verification + outbox
    S-->>T: payment.verify
    T-->>S: payment.verified / rejected
    S->>SD: enforce sender invariant + activate + outbox
    S-->>B: session.activated / activation.rejected
    B->>DB: update active projection
    B-->>U: результат
```

Уведомление `payment.confirmed` не активирует сессию напрямую: авторитетное решение появляется только после повторной проверки active-sessions.

## Повторная активация

Повторная активация проходит тот же flow. Active-sessions в одной транзакции блокирует запись balance identity, сравнивает canonical sender wallet с первым значением и только затем создаёт новую active session. Несовпадение публикуется как доменный отказ без изменения сохранённого wallet.

## Список сессий

```mermaid
sequenceDiagram
    participant U as User
    participant B as rw-bot
    participant S as rw-active-sessions
    U->>B: «Мои сессии»
    B-->>S: sessions.list.requested(requester_session_id)
    S-->>B: sessions.listed(correlation_id, sessions[])
    B-->>U: безопасное представление списка
```

Bot не строит авторитетный список из своей projection.

## Отзыв сессии

```mermaid
sequenceDiagram
    participant U as User
    participant B as rw-bot
    participant S as rw-active-sessions
    participant DB as Sessions DB
    U->>B: выбрать session + подтвердить
    B-->>S: session.revoke.requested(requester, target)
    S->>DB: authorize + conditional ACTIVE→REVOKED + outbox
    S-->>B: session.revoked / revoke.rejected
    B-->>U: результат
```

## Состояния сессии бота

```mermaid
stateDiagram-v2
    [*] --> AwaitingInvite
    AwaitingInvite --> BoundInactive: valid invite
    BoundInactive --> AwaitingSender: start activation
    AwaitingSender --> AwaitingReservation: wallet accepted
    AwaitingSender --> BoundInactive: cancel input
    AwaitingReservation --> AwaitingSender: retry later / invalid
    AwaitingReservation --> AwaitingPayment: reserved
    AwaitingPayment --> AwaitingVerification: payment claim received
    AwaitingPayment --> BoundInactive: expired
    AwaitingVerification --> Active: authoritative activation
    AwaitingVerification --> BoundInactive: rejected/expired
    Active --> Revoked: revoke event
    Revoked --> [*]
```

Каждый переход выполняется compare-and-set по текущему state/version. Late event допустим только если совпадает `activation_attempt_id` и переход разрешён.

## Состояния проверки активации

```mermaid
stateDiagram-v2
    [*] --> Pending
    Pending --> Verifying: command persisted
    Verifying --> Verified: top-up verified
    Verifying --> Rejected: mismatch/final failure
    Verifying --> Expired: deadline exceeded
    Verified --> Activated: invariants committed
    Activated --> [*]
    Rejected --> [*]
    Expired --> [*]
```

Повтор любого входного сообщения возвращает сохранённый terminal result и не повторяет доменный side effect.
