-- +goose Up
ALTER TABLE agent_audit_events
    ADD COLUMN agent_definition_digest TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE agent_audit_events
    DROP COLUMN agent_definition_digest;
