package domain

// Error codes are stable, externally visible identifiers. Protocol adapters
// and audit records reference these constants; never use bare strings.
// Codes are grouped by the layer that raises them.
const (
	// Domain validation codes (internal/domain)
	CodeExpertRequired           = "expert_required"
	CodeSystemRequired           = "system_required"
	CodeConclusionRequired       = "conclusion_required"
	CodeInvalidConfidence        = "invalid_confidence"
	CodeLLMCallIDRequired        = "llm_call_id_required"
	CodeRunIDRequired            = "run_id_required"
	CodeThreadIDRequired         = "thread_id_required"
	CodeUserIDRequired           = "user_id_required"
	CodeAgentNameRequired        = "agent_name_required"
	CodeModelRequired            = "model_required"
	CodeLLMCallStartedAtRequired = "llm_call_started_at_required"
	CodeLLMCallFailed            = "llm_call_failed"

	// Runtime initialization codes (internal/agent constructor)
	CodeLLMModelMissing        = "llm_model_missing"
	CodeEngineToolsEmpty       = "engine_tools_empty"
	CodeEngineMCPURLMissing    = "engine_mcp_url_missing"
	CodeLLMUnavailable         = "llm_unavailable"
	CodeRuntimeInitFailed      = "runtime_init_failed"
	CodeEngineToolsUnavailable = "engine_tools_unavailable"

	// Runtime execution codes (internal/agent Run / audit)
	CodeRuntimeCancelled          = "runtime_cancelled"
	CodeRuntimeTimeout            = "runtime_timeout"
	CodeRuntimeFailed             = "runtime_failed"
	CodeRuntimeEmptyResponse      = "runtime_empty_response"
	CodeRuntimeSessionUnavailable = "runtime_session_unavailable"
	CodeRuntimeInterrupted        = "runtime_interrupted"
	CodeStructuredOutputInvalid   = "structured_output_invalid"
	CodeStructuredOutputEmpty     = "structured_output_empty"

	// Audit lifecycle codes (internal/agent BeginAuditRun/EndAuditRun)
	CodeAuditScopeRequired   = "audit_scope_required"
	CodeAuditSessionRequired = "audit_session_required"
	CodeAuditSessionActive   = "audit_session_active"

	// Storage codes (internal/audit/sqlite)
	CodeAuditStoreFailed = "audit_store_failed"

	// Transport / protocol error codes
	CodeUnauthorized           = "unauthorized"
	CodeIdentityRequired       = "identity_required"
	CodeMethodNotAllowed       = "method_not_allowed"
	CodeInvalidAGUIRequest     = "invalid_agui_request"
	CodeUnsupportedAGUIFeature = "unsupported_agui_feature"
	CodeInvalidAGUIMessage     = "invalid_agui_message"
)

// Chat Completions model codes (internal/agent/chat_model.go)
const (
	CodeLLMRequestInvalid  = "llm_request_invalid"
	CodeLLMResponseInvalid = "llm_response_invalid"
)
