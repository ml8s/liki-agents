package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	agentruntime "github.com/liki/liki-agent/internal/agent"
	"github.com/liki/liki-agent/internal/audit/sqlite"
	"github.com/liki/liki-agent/internal/observability/prometheus"
	"github.com/liki/liki-agent/internal/platform"
	"github.com/liki/liki-agent/internal/platform/buildinfo"
	"github.com/liki/liki-agent/internal/platform/config"
	a2aadapter "github.com/liki/liki-agent/internal/protocol/a2a"
	aguiadapter "github.com/liki/liki-agent/internal/protocol/agui"
	"github.com/liki/liki-agent/internal/transport"
)

func main() {
	if err := run(); err != nil {
		slog.Error("liki-agent exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := newLogger(cfg)
	slog.SetDefault(logger)

	store, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()
	metricsCollector := prometheus.New()
	runtime, err := agentruntime.NewRuntime(agentruntime.Config{
		AppName:          "liki-agent",
		AgentName:        cfg.AgentName,
		AgentDescription: cfg.AgentDescription,
		ExpertName:       cfg.ExpertName,
		System:           cfg.ExpertSystem,
		Model:            cfg.LLMModel,
		ModelBaseURL:     cfg.LLMBaseURL,
		ModelAPIKey:      cfg.LLMAPIKey,
		ModelTimeout:     cfg.LLMTimeout,
		Temperature:      cfg.LLMTemperature,
		LLMRecorder:      sqlite.NewLLMCallRepository(store.GORM()),
		Metrics:          metricsCollector,
		Provider:         cfg.LLMProvider,
		Env:              cfg.Env,
		ContractVersion:  cfg.EngineContract,
		GraphVersion:     buildinfo.GraphVersion,
		PromptVersion:    cfg.PromptVersion,
		PolicyVersion:    cfg.PolicyVersion,
		AllowedTools:     cfg.EngineTools,
		EngineMCPURL:     cfg.EngineMCPURL,
		EngineToken:      cfg.EngineToken,
		EngineTimeout:    cfg.EngineTimeout,
	})
	if err != nil {
		return err
	}

	publicURL, err := url.Parse(cfg.PublicURL)
	if err != nil {
		return err
	}
	a2aServer, err := a2aadapter.New(runtime, a2aadapter.Config{
		PublicURL:  publicURL,
		RunTimeout: cfg.RunTimeout,
	})
	if err != nil {
		return err
	}
	aguiHandler, err := aguiadapter.New(runtime, aguiadapter.Config{RunTimeout: cfg.RunTimeout, Metrics: metricsCollector})
	if err != nil {
		return err
	}
	protocolServer := transport.New(transport.Services{
		AgentCard:     a2aServer.AgentCardHandler(),
		A2A:           a2aServer.EndpointHandler(),
		AGUI:          aguiHandler,
		HealthChecks:  []platform.HealthChecker{store, runtime},
		Metrics:       metricsCollector,
		Dependencies:  metricsCollector,
		MetricsTarget: metricsCollector.Handler(),
		InternalToken: cfg.InternalToken,
		Logger:        logger,
	})

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           protocolServer,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		logger.Info("liki-agent listening",
			"addr", cfg.Addr,
			"public_url", cfg.PublicURL,
			"protocols", "A2A,AG-UI,MCP",
			"env", cfg.Env,
			"engine", cfg.EngineMCPURL,
			"graph", buildinfo.GraphVersion,
		)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		drainCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(drainCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
