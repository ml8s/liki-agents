package transport_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liki/liki-agent/internal/observability"
	"github.com/liki/liki-agent/internal/platform"
	"github.com/liki/liki-agent/internal/transport"
)

type healthy struct{}

func (healthy) CheckHealth(context.Context) platform.DependencyHealth {
	return platform.DependencyHealth{Name: "test", OK: true}
}

func newServer(token string) http.Handler {
	return transport.New(transport.Services{
		AgentCard:     http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		A2A:           http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		AGUI:          http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
		HealthChecks:  []platform.HealthChecker{healthy{}},
		InternalToken: token,
	})
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
	handler := transport.New(transport.Services{
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
	handler := newServer("secret")
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
	handler := newServer("")
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
	handler := newServer("secret")
	for _, path := range []string{"/healthz", "/readyz", "/version"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, response.Code)
		}
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
	handler := transport.New(transport.Services{
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

func TestReadyzDoesNotLeakDependencyDetail(t *testing.T) {
	unhealthy := &unhealthyChecker{name: "engine_mcp", detail: "dial tcp 10.0.0.1:18081: connection refused"}
	handler := transport.New(transport.Services{
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

type unhealthyChecker struct {
	name   string
	detail string
}

func (c *unhealthyChecker) CheckHealth(context.Context) platform.DependencyHealth {
	return platform.DependencyHealth{Name: c.name, OK: false, Detail: c.detail}
}
