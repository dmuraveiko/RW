-- +goose Up
CREATE TABLE service_metadata (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    service_name text NOT NULL CHECK (service_name = 'rw-bot'),
    created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO service_metadata (service_name) VALUES ('rw-bot');

CREATE TABLE telegram_updates (
    transport text NOT NULL CHECK (transport IN ('direct_polling', 'direct_webhook', 'natsproxy')),
    update_id bigint NOT NULL CHECK (update_id >= 0),
    status text NOT NULL CHECK (status IN ('PROCESSING', 'DELIVERED', 'OUTCOME_UNKNOWN', 'IGNORED')),
    failure_code text,
    payload_digest bytea NOT NULL CHECK (octet_length(payload_digest) = 32),
    received_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    PRIMARY KEY (transport, update_id),
    CHECK ((status = 'PROCESSING') = (processed_at IS NULL))
);

CREATE INDEX telegram_updates_received_at_idx ON telegram_updates (received_at);

-- +goose Down
DROP TABLE telegram_updates;
DROP TABLE service_metadata;
