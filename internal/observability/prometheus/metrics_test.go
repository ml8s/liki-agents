package prometheus

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsExposeStandardRuntimeDimensions(t *testing.T) {
	metrics := New()
	metrics.ObserveHTTPRequest("ag_ui", "run", "200", 25*time.Millisecond)
	metrics.StreamOpened("ag_ui")
	metrics.ObserveLLMCall("gpt-test", "completed", 10, 2, 12)
	metrics.SetDependencyReady("engine_mcp", true)

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, name := range []string{
		"liki_agent_protocol_requests_total",
		"liki_agent_protocol_request_duration_seconds",
		"liki_agent_protocol_active_streams",
		"liki_agent_llm_calls_total",
		"liki_agent_llm_tokens_total",
		"liki_agent_dependency_up",
	} {
		if !strings.Contains(response.Body.String(), name) {
			t.Fatalf("metrics body missing %q", name)
		}
	}
}
