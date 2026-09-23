-- +goose Up
CREATE TABLE agent_llm_calls (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    product TEXT NOT NULL DEFAULT '',
    agent_name TEXT NOT NULL,
    model TEXT NOT NULL,
    provider TEXT NOT NULL,
    status TEXT NOT NULL,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    thought_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    graph_version TEXT NOT NULL DEFAULT '',
    contract_version TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    policy_version TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP NOT NULL DEFAULT '0001-01-01 00:00:00+00:00'
);

CREATE INDEX idx_agent_llm_calls_run_id ON agent_llm_calls(run_id);
CREATE INDEX idx_agent_llm_calls_thread_id ON agent_llm_calls(thread_id);
CREATE INDEX idx_agent_llm_calls_user_started ON agent_llm_calls(user_id, started_at);
CREATE INDEX idx_agent_llm_calls_model_started ON agent_llm_calls(model, started_at);

-- +goose Down
DROP TABLE agent_llm_calls;
