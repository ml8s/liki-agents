// Package transport composes standard protocol and operational endpoints.
package transport

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ml8s/liki-agents/internal/domain"
	"github.com/ml8s/liki-agents/internal/observability"
	"github.com/ml8s/liki-agents/internal/platform"
	"github.com/ml8s/liki-agents/internal/platform/buildinfo"
	"github.com/ml8s/liki-agents/internal/platform/identity"
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
	routes   *http.ServeMux
}

func New(services Services) (*Server, error) {
	if services.AgentCard == nil || services.A2A == nil || services.AGUI == nil {
		return nil, fmt.Errorf("A2A and AG-UI protocol handlers are required")
	}
	if services.Logger == nil {
		services.Logger = slog.Default()
	}

	server := &Server{services: services}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", server.ready)
	mux.HandleFunc("GET /version", server.version)
	mux.Handle("GET /metrics", server.metricsHandler())
	mux.Handle("GET /.well-known/agent-card.json", server.services.AgentCard)
	mux.Handle("POST /a2a", server.authorized(server.bodyLimit(server.services.A2A), false))
	mux.Handle("POST /ag-ui", server.authorized(server.bodyLimit(server.services.AGUI), true))

	server.routes = mux
	return server, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := r.Header.Get("X-Request-ID")
	if !validRequestID(requestID) {
		requestID = randomID()
	}
	r = r.WithContext(identity.WithIdentity(r.Context(), verifiedIdentity(r)))
	writer := &responseWriter{ResponseWriter: w, status: http.StatusOK}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Content-Type-Options", "nosniff")

	s.routes.ServeHTTP(writer, r)
	s.observe(r.URL.Path, r.Method, writer.status, time.Since(start))
}

func (s *Server) bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		next.ServeHTTP(w, r)
	})
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

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	// A disconnected readiness client is not a dependency failure. Health
	// checks own their deadlines and must run with request values but without
	// the request's cancellation signal.
	checkContext := context.WithoutCancel(r.Context())
	healthResults := make([]platform.DependencyHealth, len(s.services.HealthChecks))
	var wait sync.WaitGroup
	for index, checker := range s.services.HealthChecks {
		wait.Add(1)
		go func() {
			defer wait.Done()
			healthResults[index] = checker.CheckHealth(checkContext)
		}()
	}
	wait.Wait()

	unhealthy := make([]string, 0)
	for _, health := range healthResults {
		if s.services.Dependencies != nil {
			s.services.Dependencies.SetDependencyReady(health.Name, health.OK)
		}
		if !health.OK {
			s.services.Logger.Warn("readiness dependency unhealthy",
				"dependency", health.Name, "detail", health.Detail)
			unhealthy = append(unhealthy, health.Name)
		}
	}
	if len(unhealthy) != 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":       "unavailable",
			"dependencies": unhealthy,
		})
		return
	}
	if s.services.Dependencies != nil {
		s.services.Dependencies.SetDependencyReady("runtime", true)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":       "liki-agents",
		"version":       buildinfo.Version,
		"commit":        buildinfo.Commit,
		"build_time":    buildinfo.BuildTime,
		"graph_version": buildinfo.GraphVersion,
		"protocols":     []string{"a2a", "ag-ui", "mcp-client"},
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
	if !validRequestID(userID) {
		return identity.Identity{}
	}
	return identity.Identity{UserID: userID}
}

func validRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !unicode.IsPrint(char) {
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
