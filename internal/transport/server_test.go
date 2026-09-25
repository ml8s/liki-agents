package transport_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ml8s/liki-agents/internal/observability"
	"github.com/ml8s/liki-agents/internal/platform"
	"github.com/ml8s/liki-agents/internal/transport"
)

type healthy struct{}

func (healthy) CheckHealth(context.Context) platform.DependencyHealth {
	return platform.DependencyHealth{Name: "test", OK: true}
}

func newServer(t *testing.T, token string) http.Handler {
	t.Helper()
	handler, err := transport.New(transport.Services{
		AgentCard:     http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		A2A:           http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		AGUI:          http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		HealthChecks:  []platform.HealthChecker{healthy{}},
		InternalToken: token,
	})
	if err != nil {
		t.Fatalf("transport.New() error = %v", err)
	}
	return handler
}

func mustNewServer(t *testing.T, services transport.Services) http.Handler {
	t.Helper()
	handler, err := transport.New(services)
	if err != nil {
		t.Fatalf("transport.New() error = %v", err)
	}
	return handler
}

type recordingMetrics struct {
	requests []string
}

var _ observability.ProtocolMetrics = (*recordingMetrics)(nil)

func (m *recordingMetrics) ObserveHTTPRequest(protocol, operation, status string, _ time.Duration) {
	m.requests = append(m.requests, protocol+"/"+operation+"/"+status)
}

func (*recordingMetrics) StreamOpened(string) {}
func (*recordingMetrics) StreamClosed(string) {}

func TestProtocolRequestsAreObserved(t *testing.T) {
	metrics := &recordingMetrics{}
	handler := mustNewServer(t, transport.Services{
		AgentCard: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		A2A:       http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		AGUI:      http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		Metrics:   metrics,
	})
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil))
	request := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(`{}`))
	request.Header.Set("X-Liki-User-ID", "user_1")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if len(metrics.requests) != 2 ||
		metrics.requests[0] != "a2a/agent_card/200" ||
		metrics.requests[1] != "a2a/message/202" {
		t.Fatalf("metrics = %v", metrics.requests)
	}
}

func TestProtocolRoutesRequireServiceToken(t *testing.T) {
	handler := newServer(t, "secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(`{}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("authenticated status = %d", response.Code)
	}
}

func TestAGUIRequiresVerifiedIdentity(t *testing.T) {
	handler := newServer(t, "")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/ag-ui", strings.NewReader(`{}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("identityless status = %d", response.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/ag-ui", strings.NewReader(`{}`))
	request.Header.Set("X-Liki-User-ID", "user_1")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("identified status = %d", response.Code)
	}
}

func TestOperationalEndpoints(t *testing.T) {
	handler := newServer(t, "secret")
	for _, path := range []string{"/readyz", "/version"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, response.Code)
		}
	}
}

func TestHealthzIsRetired(t *testing.T) {
	handler := newServer(t, "")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("healthz status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestA2ABodySizeLimit(t *testing.T) {
	readBody := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	handler := mustNewServer(t, transport.Services{
		AgentCard: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		A2A:       readBody,
		AGUI:      http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
	})
	bigBody := strings.Repeat("x", 2<<20+1)
	request := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(bigBody))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("body too large status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestAuthenticationRunsBeforeRequestBodyIsConsumed(t *testing.T) {
	read := false
	handler := mustNewServer(t, transport.Services{
		AgentCard: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		A2A: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			read = true
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusAccepted)
		}),
		AGUI:          http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		InternalToken: "secret",
	})
	request := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(strings.Repeat("x", 2<<20+1)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated oversized status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if read {
		t.Fatal("protocol handler read an unauthenticated request body")
	}
}

func TestAGUIRejectsOversizedIdentityHeader(t *testing.T) {
	handler := newServer(t, "")
	request := httptest.NewRequest(http.MethodPost, "/ag-ui", strings.NewReader(`{}`))
	request.Header.Set("X-Liki-User-ID", strings.Repeat("u", 129))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("oversized identity status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestReadyzDoesNotLeakDependencyDetail(t *testing.T) {
	unhealthy := &unhealthyChecker{name: "engine_mcp", detail: "dial tcp 10.0.0.1:18081: connection refused"}
	handler := mustNewServer(t, transport.Services{
		AgentCard:    http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		A2A:          http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		AGUI:         http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		HealthChecks: []platform.HealthChecker{unhealthy},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	body := response.Body.String()
	if strings.Contains(body, "dial tcp") || strings.Contains(body, "connection refused") {
		t.Fatalf("readyz leaks internal detail: %s", body)
	}
	if !strings.Contains(body, "engine_mcp") {
		t.Fatalf("readyz should mention dependency name: %s", body)
	}
}

func TestReadyzChecksEveryDependency(t *testing.T) {
	handler := mustNewServer(t, transport.Services{
		AgentCard: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		A2A:       http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		AGUI:      http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		HealthChecks: []platform.HealthChecker{
			&unhealthyChecker{name: "first", detail: "first failure"},
			healthy{},
			&unhealthyChecker{name: "second", detail: "second failure"},
		},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, "first") || !strings.Contains(body, "second") {
		t.Fatalf("readyz omitted a failed dependency: %s", body)
	}
	if strings.Contains(body, "first failure") || strings.Contains(body, "second failure") {
		t.Fatalf("readyz leaks dependency detail: %s", body)
	}
}

func TestReadyzDoesNotInheritClientCancellation(t *testing.T) {
	handler := mustNewServer(t, transport.Services{
		AgentCard:    http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		A2A:          http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		AGUI:         http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		HealthChecks: []platform.HealthChecker{contextAwareChecker{}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx))

	if response.Code != http.StatusOK {
		t.Fatalf("canceled readiness status = %d, want %d", response.Code, http.StatusOK)
	}
}

type unhealthyChecker struct {
	name   string
	detail string
}

type contextAwareChecker struct{}

func (contextAwareChecker) CheckHealth(ctx context.Context) platform.DependencyHealth {
	return platform.DependencyHealth{Name: "context", OK: ctx.Err() == nil}
}

func (c *unhealthyChecker) CheckHealth(context.Context) platform.DependencyHealth {
	return platform.DependencyHealth{Name: c.name, OK: false, Detail: c.detail}
}
