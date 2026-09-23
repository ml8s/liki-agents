// Package transport composes standard protocol and operational endpoints.
package transport

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/liki/liki-agent/internal/domain"
	"github.com/liki/liki-agent/internal/observability"
	"github.com/liki/liki-agent/internal/platform"
	"github.com/liki/liki-agent/internal/platform/buildinfo"
	"github.com/liki/liki-agent/internal/platform/identity"
)

type Services struct {
	AgentCard     http.Handler
	A2A           http.Handler
	AGUI          http.Handler
	HealthChecks  []platform.HealthChecker
	Metrics       observability.ProtocolMetrics
	Dependencies  observability.DependencyMetrics
	MetricsTarget http.Handler
	InternalToken string
	Logger        *slog.Logger
}

type Server struct {
	services Services
}

func New(services Services) *Server {
	if services.AgentCard == nil || services.A2A == nil || services.AGUI == nil {
		panic("A2A and AG-UI protocol handlers are required")
	}
	if services.Logger == nil {
		services.Logger = slog.Default()
	}
	return &Server{services: services}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = randomID()
	}
	r = r.WithContext(identity.WithIdentity(r.Context(), verifiedIdentity(r)))
	writer := &responseWriter{ResponseWriter: w, status: http.StatusOK}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Content-Type-Options", "nosniff")

	var handler http.Handler
	switch {
	case r.URL.Path == "/healthz" && r.Method == http.MethodGet:
		handler = http.HandlerFunc(s.health)
	case r.URL.Path == "/readyz" && r.Method == http.MethodGet:
		handler = http.HandlerFunc(s.ready)
	case r.URL.Path == "/version" && r.Method == http.MethodGet:
		handler = http.HandlerFunc(s.version)
	case r.URL.Path == "/metrics" && r.Method == http.MethodGet:
		handler = s.metricsHandler()
	case r.URL.Path == "/.well-known/agent-card.json" && r.Method == http.MethodGet:
		handler = s.services.AgentCard
	case r.URL.Path == "/a2a" && r.Method == http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		handler = s.authorized(s.services.A2A, false)
	case r.URL.Path == "/ag-ui" && r.Method == http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		handler = s.authorized(s.services.AGUI, true)
	default:
		handler = http.NotFoundHandler()
	}
	handler.ServeHTTP(writer, r)
	s.observe(r.URL.Path, r.Method, writer.status, time.Since(start))
}

func (s *Server) observe(path, method string, status int, duration time.Duration) {
	if s.services.Metrics == nil {
		return
	}
	protocol, operation, ok := protocolOperation(path, method)
	if !ok {
		return
	}
	s.services.Metrics.ObserveHTTPRequest(protocol, operation, strconv.Itoa(status), duration)
}

func protocolOperation(path, method string) (protocol, operation string, ok bool) {
	switch {
	case path == "/a2a" && method == http.MethodPost:
		return "a2a", "message", true
	case path == "/ag-ui" && method == http.MethodPost:
		return "ag_ui", "run", true
	case path == "/.well-known/agent-card.json" && method == http.MethodGet:
		return "a2a", "agent_card", true
	default:
		return "", "", false
	}
}

func (s *Server) authorized(next http.Handler, requireIdentity bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.services.InternalToken != "" {
			const prefix = "Bearer "
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, prefix) || subtle.ConstantTimeCompare([]byte(header[len(prefix):]), []byte(s.services.InternalToken)) != 1 {
				writeError(w, http.StatusUnauthorized, domain.CodeUnauthorized, "valid service token required")
				return
			}
		}
		verified, _ := identity.FromContext(r.Context())
		if requireIdentity && verified.IsZero() {
			writeError(w, http.StatusUnauthorized, domain.CodeIdentityRequired, "verified user identity required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	for _, checker := range s.services.HealthChecks {
		health := checker.CheckHealth(r.Context())
		if s.services.Dependencies != nil {
			s.services.Dependencies.SetDependencyReady(health.Name, health.OK)
		}
		if !health.OK {
			s.services.Logger.Warn("readiness dependency unhealthy",
				"dependency", health.Name, "detail", health.Detail)
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status":     "unavailable",
				"dependency": health.Name,
			})
			return
		}
	}
	if s.services.Dependencies != nil {
		s.services.Dependencies.SetDependencyReady("runtime", true)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":       "liki-agent",
		"version":       buildinfo.Version,
		"commit":        buildinfo.Commit,
		"build_time":    buildinfo.BuildTime,
		"graph_version": buildinfo.GraphVersion,
		"protocols":     []string{"a2a", "ag-ui", "mcp"},
	})
}

func (s *Server) metricsHandler() http.Handler {
	if s.services.MetricsTarget != nil {
		return s.services.MetricsTarget
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})
}

func verifiedIdentity(r *http.Request) identity.Identity {
	userID := strings.TrimSpace(r.Header.Get("X-Liki-User-ID"))
	if userID == "" {
		return identity.Identity{}
	}
	return identity.Identity{UserID: userID}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
