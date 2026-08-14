-- +goose Up
ALTER TABLE message_inbox DROP CONSTRAINT message_inbox_status_check;
ALTER TABLE message_inbox ADD CONSTRAINT message_inbox_status_check
    CHECK (status IN ('PROCESSING', 'COMPLETED', 'APPLIED', 'REJECTED'));

CREATE TABLE invites (
    id uuid PRIMARY KEY,
    operation_id uuid NOT NULL UNIQUE,
    command_message_id uuid NOT NULL UNIQUE REFERENCES message_inbox(message_id),
    result_message_id uuid NOT NULL UNIQUE REFERENCES message_outbox(message_id),
    token_digest bytea NOT NULL UNIQUE CHECK (octet_length(token_digest) = 32),
    token_ciphertext bytea NOT NULL,
    balance_id_ciphertext bytea NOT NULL,
    balance_id_fingerprint bytea NOT NULL CHECK (octet_length(balance_id_fingerprint) = 32),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'CONSUMED', 'EXPIRED')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((status = 'CONSUMED') = (consumed_at IS NOT NULL))
);

CREATE INDEX invites_expiry_idx ON invites (expires_at) WHERE status = 'ACTIVE';

CREATE TABLE inactive_sessions (
    id uuid PRIMARY KEY,
    bot_id bigint NOT NULL CHECK (bot_id > 0),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    telegram_chat_id bigint NOT NULL CHECK (telegram_chat_id > 0),
    balance_id_ciphertext bytea NOT NULL,
    balance_id_fingerprint bytea NOT NULL CHECK (octet_length(balance_id_fingerprint) = 32),
    display_label text CHECK (display_label IS NULL OR length(display_label) BETWEEN 1 AND 128),
    dialog_state text NOT NULL CHECK (dialog_state IN ('AWAITING_WALLET', 'AWAITING_RESERVATION', 'AWAITING_PAYMENT', 'AWAITING_VERIFICATION', 'REJECTED')),
    current_attempt_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE (bot_id, telegram_user_id)
);

CREATE INDEX inactive_sessions_expiry_idx ON inactive_sessions (expires_at, updated_at);

ALTER TABLE invites ADD COLUMN consumed_by_session_id uuid REFERENCES inactive_sessions(id) ON DELETE SET NULL;

CREATE TABLE activation_attempts (
    id uuid PRIMARY KEY,
    operation_id uuid NOT NULL UNIQUE,
    session_id uuid NOT NULL,
    reserve_message_id uuid NOT NULL UNIQUE REFERENCES message_outbox(message_id),
    verify_message_id uuid UNIQUE REFERENCES message_outbox(message_id),
    sender_wallet_ciphertext bytea NOT NULL,
    sender_wallet_fingerprint bytea NOT NULL CHECK (octet_length(sender_wallet_fingerprint) = 32),
    receiver_wallet_ciphertext bytea,
    receiver_wallet_fingerprint bytea CHECK (receiver_wallet_fingerprint IS NULL OR octet_length(receiver_wallet_fingerprint) = 32),
    verification_amount text NOT NULL,
    asset text NOT NULL CHECK (asset = 'USDT'),
    network text NOT NULL CHECK (length(network) BETWEEN 1 AND 64),
    external_reservation_id text,
    transaction_id text,
    offer_valid_from timestamptz,
    offer_expires_at timestamptz,
    status text NOT NULL CHECK (status IN ('AWAITING_RESERVATION', 'AWAITING_PAYMENT', 'AWAITING_VERIFICATION', 'ACTIVE', 'REJECTED', 'EXPIRED')),
    failure_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    CHECK ((status IN ('ACTIVE', 'REJECTED', 'EXPIRED')) = (completed_at IS NOT NULL))
);

CREATE INDEX activation_attempts_pending_idx ON activation_attempts (status, updated_at)
    WHERE status IN ('AWAITING_RESERVATION', 'AWAITING_PAYMENT', 'AWAITING_VERIFICATION');
CREATE UNIQUE INDEX activation_attempts_transaction_idx ON activation_attempts (network, transaction_id)
    WHERE transaction_id IS NOT NULL;

ALTER TABLE inactive_sessions
    ADD CONSTRAINT inactive_sessions_current_attempt_fk
    FOREIGN KEY (current_attempt_id) REFERENCES activation_attempts(id);

CREATE TABLE active_session_projection (
    session_id uuid PRIMARY KEY,
    bot_id bigint NOT NULL CHECK (bot_id > 0),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    telegram_chat_id bigint NOT NULL CHECK (telegram_chat_id > 0),
    balance_id_fingerprint bytea NOT NULL CHECK (octet_length(balance_id_fingerprint) = 32),
    display_label text CHECK (display_label IS NULL OR length(display_label) BETWEEN 1 AND 128),
    authority_version bigint NOT NULL CHECK (authority_version > 0),
    activated_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (bot_id, telegram_user_id)
);

-- +goose Down
DROP TABLE active_session_projection;
ALTER TABLE inactive_sessions DROP CONSTRAINT inactive_sessions_current_attempt_fk;
DROP TABLE activation_attempts;
ALTER TABLE invites DROP COLUMN consumed_by_session_id;
DROP TABLE inactive_sessions;
DROP TABLE invites;
ALTER TABLE message_inbox DROP CONSTRAINT message_inbox_status_check;
ALTER TABLE message_inbox ADD CONSTRAINT message_inbox_status_check
    CHECK (status IN ('PROCESSING', 'COMPLETED', 'REJECTED'));
