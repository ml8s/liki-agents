package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	Env                 string
	Addr                string
	PublicURL           string
	InternalToken       string
	DataDir             string
	DBPath              string
	ToolContract        string
	DeploymentFile      string
	MCPTimeout          time.Duration
	MaxConcurrentRuns   int
	RunTimeout          time.Duration
	ShutdownTimeout     time.Duration
	LLMStructuredOutput string
	LLMBaseURL          string
	LLMAPIKey           string
	LLMModel            string
	LLMProvider         string
	LLMTimeout          time.Duration
	LLMTemperature      float64
	LogFormat           string
	LogLevel            string
}

func Load() (Config, error) {
	cfg := Config{
		Env:                 getEnv("LIKI_ENV", "development"),
		Addr:                getEnv("LIKI_AGENTS_ADDR", ":8083"),
		PublicURL:           getEnv("LIKI_AGENTS_PUBLIC_URL", "http://127.0.0.1:8083"),
		InternalToken:       getEnv("LIKI_AGENTS_INTERNAL_TOKEN", ""),
		DataDir:             getEnv("LIKI_AGENTS_DATA_DIR", "./data"),
		DBPath:              getEnv("LIKI_DB_PATH", ""),
		ToolContract:        getEnv("LIKI_TOOL_CONTRACT_VERSION", ""),
		DeploymentFile:      getEnv("LIKI_AGENTS_DEPLOYMENT_FILE", ""),
		MaxConcurrentRuns:   32,
		LLMBaseURL:          getEnv("LIKI_LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMAPIKey:           getEnv("LIKI_LLM_API_KEY", ""),
		LLMModel:            getEnv("LIKI_LLM_MODEL", "gpt-4.1-mini"),
		LLMProvider:         getEnv("LIKI_LLM_PROVIDER", ""),
		LLMStructuredOutput: getEnv("LIKI_LLM_STRUCTURED_OUTPUT", ""),
		LogFormat:           getEnv("LIKI_LOG_FORMAT", "json"),
		LogLevel:            getEnv("LIKI_LOG_LEVEL", "info"),
	}
	if cfg.LLMStructuredOutput == "none" {
		cfg.LLMStructuredOutput = ""
	}
	var err error
	if cfg.MCPTimeout, err = getDuration("LIKI_MCP_TIMEOUT_SECONDS", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.MaxConcurrentRuns, err = getInt("LIKI_MAX_CONCURRENT_RUNS", 32); err != nil {
		return Config{}, err
	}
	if cfg.RunTimeout, err = getDuration("LIKI_RUN_TIMEOUT_SECONDS", 600*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = getDuration("LIKI_SHUTDOWN_TIMEOUT_SECONDS", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.LLMTimeout, err = getDuration("LIKI_LLM_TIMEOUT_SECONDS", 120*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.LLMTemperature, err = getFloat("LIKI_LLM_TEMPERATURE", 0.2); err != nil {
		return Config{}, err
	}
	return validate(cfg)
}

func validate(cfg Config) (Config, error) {
	if cfg.ToolContract == "" {
		return Config{}, fmt.Errorf("LIKI_TOOL_CONTRACT_VERSION is required")
	}
	if cfg.Env != "development" && cfg.InternalToken == "" {
		return Config{}, fmt.Errorf("LIKI_AGENTS_INTERNAL_TOKEN is required outside development")
	}
	if cfg.Env != "development" && cfg.LLMAPIKey == "" {
		return Config{}, fmt.Errorf("LIKI_LLM_API_KEY is required outside development")
	}
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(cfg.DataDir, "liki-agents-audit.db")
	}
	if cfg.DeploymentFile == "" {
		return Config{}, fmt.Errorf("LIKI_AGENTS_DEPLOYMENT_FILE is required")
	}
	switch cfg.LLMProvider {
	case "openai", "zhipu", "bigmodel", "glm":
	default:
		return Config{}, fmt.Errorf("LIKI_LLM_PROVIDER is unsupported: %q", cfg.LLMProvider)
	}
	switch cfg.LLMStructuredOutput {
	case "", "none", "json_schema", "json_object":
	default:
		return Config{}, fmt.Errorf("LIKI_LLM_STRUCTURED_OUTPUT is unsupported: %q", cfg.LLMStructuredOutput)
	}
	if cfg.RunTimeout <= 0 {
		return Config{}, fmt.Errorf("LIKI_RUN_TIMEOUT_SECONDS must be greater than zero")
	}
	if cfg.MCPTimeout <= 0 {
		return Config{}, fmt.Errorf("LIKI_MCP_TIMEOUT_SECONDS must be greater than zero")
	}
	if cfg.MaxConcurrentRuns <= 0 {
		return Config{}, fmt.Errorf("LIKI_MAX_CONCURRENT_RUNS must be greater than zero")
	}
	if cfg.LLMTimeout <= 0 {
		return Config{}, fmt.Errorf("LIKI_LLM_TIMEOUT_SECONDS must be greater than zero")
	}
	if cfg.LLMTemperature < 0 || cfg.LLMTemperature > 2 ||
		math.IsNaN(cfg.LLMTemperature) || math.IsInf(cfg.LLMTemperature, 0) {
		return Config{}, fmt.Errorf("LIKI_LLM_TEMPERATURE must be between 0 and 2")
	}
	if _, err := url.Parse(cfg.PublicURL); err != nil {
		return Config{}, fmt.Errorf("LIKI_AGENTS_PUBLIC_URL is invalid: %w", err)
	}
	if cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("LIKI_SHUTDOWN_TIMEOUT_SECONDS must be greater than zero")
	}
	publicURL, err := url.Parse(cfg.PublicURL)
	if err != nil || publicURL.Scheme != "http" && publicURL.Scheme != "https" || publicURL.Host == "" {
		return Config{}, fmt.Errorf("LIKI_AGENTS_PUBLIC_URL must be an absolute HTTP(S) URL")
	}
	llmURL, err := url.Parse(cfg.LLMBaseURL)
	if err != nil || llmURL.Scheme != "http" && llmURL.Scheme != "https" || llmURL.Host == "" {
		return Config{}, fmt.Errorf("LIKI_LLM_BASE_URL must be an absolute HTTP(S) URL")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is not a valid integer: %q", key, raw)
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero, got %d", key, seconds)
	}
	return time.Duration(seconds) * time.Second, nil
}

func getFloat(key string, fallback float64) (float64, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s is not a valid number: %q", key, raw)
	}
	return value, nil
}

func getInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is not a valid integer: %q", key, raw)
	}
	return value, nil
}
