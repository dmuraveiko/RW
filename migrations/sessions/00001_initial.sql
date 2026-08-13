-- +goose Up
CREATE TABLE service_metadata (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    service_name text NOT NULL CHECK (service_name = 'rw-active-sessions'),
    created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO service_metadata (service_name) VALUES ('rw-active-sessions');

-- +goose Down
DROP TABLE service_metadata;
