-- +goose Up
CREATE TABLE message_inbox (
    message_id uuid PRIMARY KEY,
    subject text NOT NULL,
    producer text NOT NULL,
    payload_digest bytea NOT NULL CHECK (octet_length(payload_digest) = 32),
    status text NOT NULL CHECK (status IN ('PROCESSING', 'COMPLETED', 'REJECTED')),
    result_message_id uuid,
    received_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    expires_at timestamptz NOT NULL,
    CHECK ((status = 'PROCESSING') = (completed_at IS NULL)),
    CHECK ((status = 'COMPLETED') = (result_message_id IS NOT NULL))
);

CREATE INDEX message_inbox_retention_idx ON message_inbox (completed_at, expires_at)
    WHERE status <> 'PROCESSING';

CREATE TABLE message_outbox (
    message_id uuid PRIMARY KEY,
    subject text NOT NULL,
    envelope bytea NOT NULL CHECK (octet_length(envelope) BETWEEN 1 AND 400000),
    kind text NOT NULL CHECK (kind IN ('COMMAND', 'RESULT', 'EVENT')),
    status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PUBLISHING', 'PUBLISHED', 'CONFIRMED', 'DEAD')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz,
    last_error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    confirmed_at timestamptz,
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > created_at),
    CHECK ((status = 'PUBLISHING') = (lease_until IS NOT NULL)),
    CHECK ((status = 'CONFIRMED') = (confirmed_at IS NOT NULL))
);

CREATE INDEX message_outbox_due_idx ON message_outbox (next_attempt_at, created_at)
    WHERE status IN ('PENDING', 'PUBLISHED');
CREATE INDEX message_outbox_lease_idx ON message_outbox (lease_until)
    WHERE status = 'PUBLISHING';

CREATE TABLE balance_identities (
    id uuid PRIMARY KEY,
    balance_id_ciphertext bytea NOT NULL,
    balance_id_fingerprint bytea NOT NULL UNIQUE CHECK (octet_length(balance_id_fingerprint) = 32),
    sender_wallet_ciphertext bytea NOT NULL,
    sender_wallet_fingerprint bytea NOT NULL CHECK (octet_length(sender_wallet_fingerprint) = 32),
    network text NOT NULL CHECK (length(network) BETWEEN 1 AND 64),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0)
);

CREATE TABLE active_sessions (
    id uuid PRIMARY KEY,
    balance_identity_id uuid NOT NULL REFERENCES balance_identities(id),
    client_type text NOT NULL CHECK (client_type = 'TELEGRAM'),
    bot_id bigint NOT NULL CHECK (bot_id > 0),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    telegram_chat_id bigint NOT NULL CHECK (telegram_chat_id > 0),
    display_label text CHECK (display_label IS NULL OR length(display_label) BETWEEN 1 AND 128),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'REVOKED')),
    activation_network text NOT NULL,
    activation_transaction_id text NOT NULL CHECK (length(activation_transaction_id) BETWEEN 1 AND 256),
    first_activated_at timestamptz NOT NULL,
    last_activated_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoked_by_session_id uuid REFERENCES active_sessions(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (activation_network, activation_transaction_id),
    CHECK ((status = 'REVOKED') = (revoked_at IS NOT NULL))
);

CREATE UNIQUE INDEX active_sessions_telegram_identity_idx
    ON active_sessions (client_type, bot_id, telegram_user_id)
    WHERE status = 'ACTIVE';
CREATE INDEX active_sessions_list_idx
    ON active_sessions (balance_identity_id, status, last_activated_at DESC, id);

CREATE TABLE activation_verifications (
    id uuid PRIMARY KEY,
    operation_id uuid NOT NULL UNIQUE,
    command_message_id uuid NOT NULL UNIQUE REFERENCES message_inbox(message_id),
    topup_command_message_id uuid NOT NULL UNIQUE REFERENCES message_outbox(message_id),
    session_id uuid NOT NULL,
    balance_id_ciphertext bytea NOT NULL,
    balance_id_fingerprint bytea NOT NULL CHECK (octet_length(balance_id_fingerprint) = 32),
    bot_id bigint NOT NULL CHECK (bot_id > 0),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    telegram_chat_id bigint NOT NULL CHECK (telegram_chat_id > 0),
    display_label text CHECK (display_label IS NULL OR length(display_label) BETWEEN 1 AND 128),
    sender_wallet_ciphertext bytea NOT NULL,
    sender_wallet_fingerprint bytea NOT NULL CHECK (octet_length(sender_wallet_fingerprint) = 32),
    receiver_wallet_ciphertext bytea NOT NULL,
    receiver_wallet_fingerprint bytea NOT NULL CHECK (octet_length(receiver_wallet_fingerprint) = 32),
    amount text NOT NULL,
    network text NOT NULL,
    transaction_id text NOT NULL,
    external_reservation_id text NOT NULL,
    offer_expires_at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('PENDING', 'VERIFIED', 'REJECTED', 'EXPIRED', 'MANUAL_REVIEW')),
    failure_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    CHECK ((status = 'PENDING') = (completed_at IS NULL))
);

CREATE INDEX activation_verifications_reconcile_idx
    ON activation_verifications (status, updated_at)
    WHERE status = 'PENDING';

CREATE TABLE session_events (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES active_sessions(id),
    operation_id uuid NOT NULL UNIQUE,
    event_type text NOT NULL CHECK (event_type IN ('ACTIVATED', 'REACTIVATED', 'REVOKED')),
    actor_session_id uuid,
    authority_version bigint NOT NULL CHECK (authority_version > 0),
    occurred_at timestamptz NOT NULL,
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX session_events_session_idx ON session_events (session_id, occurred_at, id);

-- +goose Down
DROP TABLE session_events;
DROP TABLE activation_verifications;
DROP TABLE active_sessions;
DROP TABLE balance_identities;
DROP TABLE message_outbox;
DROP TABLE message_inbox;
