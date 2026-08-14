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

-- +goose Down
DROP TABLE message_outbox;
DROP TABLE message_inbox;
