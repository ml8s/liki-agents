-- +goose Up
CREATE TABLE agent_audit_events (
    id TEXT PRIMARY KEY,
    schema_version TEXT NOT NULL,
    event_type TEXT NOT NULL,
    occurred_at TIMESTAMP NOT NULL,
    root_run_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    parent_run_id TEXT NOT NULL DEFAULT '',
    thread_id TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL DEFAULT '',
    protocol TEXT NOT NULL DEFAULT '',
    trace_id TEXT NOT NULL DEFAULT '',
    span_id TEXT NOT NULL DEFAULT '',
    agent_name TEXT NOT NULL DEFAULT '',
    agent_version TEXT NOT NULL DEFAULT '',
    definition_name TEXT NOT NULL DEFAULT '',
    definition_version TEXT NOT NULL DEFAULT '',
    definition_digest TEXT NOT NULL DEFAULT '',
    caller_agent TEXT NOT NULL DEFAULT '',
    target_agent TEXT NOT NULL DEFAULT '',
    delegation_depth INTEGER NOT NULL DEFAULT 0 CHECK (delegation_depth >= 0),
    tool_call_id TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_agent_audit_events_run_id ON agent_audit_events(run_id);
CREATE INDEX idx_agent_audit_events_trace_id ON agent_audit_events(trace_id);
CREATE INDEX idx_agent_audit_events_root_run_id ON agent_audit_events(root_run_id);
CREATE INDEX idx_agent_audit_events_type_occurred ON agent_audit_events(event_type, occurred_at);
CREATE INDEX idx_agent_audit_events_definition ON agent_audit_events(definition_name, definition_version);
CREATE INDEX idx_agent_audit_events_agent_occurred ON agent_audit_events(agent_name, occurred_at);
CREATE INDEX idx_agent_audit_events_tool_call_id ON agent_audit_events(tool_call_id);
CREATE INDEX idx_agent_audit_events_tool_occurred ON agent_audit_events(tool_name, occurred_at);

-- +goose StatementBegin
CREATE TRIGGER agent_audit_events_no_update
BEFORE UPDATE ON agent_audit_events
BEGIN
    SELECT RAISE(ABORT, 'agent audit events are append-only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER agent_audit_events_no_delete
BEFORE DELETE ON agent_audit_events
BEGIN
    SELECT RAISE(ABORT, 'agent audit events are append-only');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS agent_audit_events_no_delete;
DROP TRIGGER IF EXISTS agent_audit_events_no_update;
DROP TABLE agent_audit_events;
