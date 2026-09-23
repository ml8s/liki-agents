package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env              string
	Addr             string
	PublicURL        string
	InternalToken    string
	DataDir          string
	DBPath           string
	EngineMCPURL     string
	EngineToken      string
	EngineContract   string
	EngineTimeout    time.Duration
	PromptVersion    string
	PolicyVersion    string
	RunTimeout       time.Duration
	ShutdownTimeout  time.Duration
	EngineTools      []string
	AgentName        string
	AgentDescription string
	ExpertName       string
	ExpertSystem     string
	LLMBaseURL       string
	LLMAPIKey        string
	LLMModel         string
	LLMProvider      string
	LLMTimeout       time.Duration
	LLMTemperature   float64
	LogFormat        string
	LogLevel         string
}

func Load() (Config, error) {
	cfg := Config{
		Env:            getEnv("LIKI_ENV", "development"),
		Addr:           getEnv("LIKI_AGENT_ADDR", ":8083"),
		PublicURL:      getEnv("LIKI_AGENT_PUBLIC_URL", "http://127.0.0.1:8083"),
		InternalToken:  getEnv("LIKI_AGENT_INTERNAL_TOKEN", ""),
		DataDir:        getEnv("LIKI_AGENT_DATA_DIR", "./data"),
		DBPath:         getEnv("LIKI_DB_PATH", ""),
		EngineMCPURL:   getEnv("LIKI_ENGINE_MCP_URL", ""),
		EngineToken:    getEnv("LIKI_ENGINE_MCP_TOKEN", ""),
		EngineContract: getEnv("LIKI_ENGINE_CONTRACT_VERSION", "unversioned"),
		PromptVersion:  getEnv("LIKI_PROMPT_VERSION", "chief-analysis-v1"),
		PolicyVersion:  getEnv("LIKI_POLICY_VERSION", "destiny-safety-v1"),
		EngineTools: splitCSV(getEnv("LIKI_ENGINE_ALLOWED_TOOLS", strings.Join([]string{
			"bazhai_chart",
			"bazhai_layout",
			"bazi_bond",
			"bazi_chart",
			"bazi_fullchart",
			"bazi_liunian",
			"bazi_liuri",
			"bazi_liushi",
			"bazi_liuyue",
			"bazi_xiaoyun",
			"city_coords",
			"huangli_days",
			"liuyao_chart",
			"liuyao_qigua",
			"qimen_chart",
			"tianwen_time",
			"time_now",
			"xuankong_chart",
			"xuankong_liunian",
			"ziwei_bond",
			"ziwei_chart",
			"ziwei_daxian",
			"ziwei_fullchart",
			"ziwei_liunian",
			"ziwei_liuri",
			"ziwei_liushi",
			"ziwei_liuyue",
		}, ","))),
		AgentName:        getEnv("LIKI_AGENT_NAME", "chief_analyst"),
		AgentDescription: getEnv("LIKI_AGENT_DESCRIPTION", "Grounded destiny analysis assistant"),
		ExpertName:       getEnv("LIKI_EXPERT_NAME", "chief_analyst"),
		ExpertSystem:     getEnv("LIKI_EXPERT_SYSTEM", "chief"),
		LLMBaseURL:       getEnv("LIKI_LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMAPIKey:        getEnv("LIKI_LLM_API_KEY", ""),
		LLMModel:         getEnv("LIKI_LLM_MODEL", "gpt-4.1-mini"),
		LLMProvider:      getEnv("LIKI_LLM_PROVIDER", ""),
		LogFormat:        getEnv("LIKI_LOG_FORMAT", "json"),
		LogLevel:         getEnv("LIKI_LOG_LEVEL", "info"),
	}
	var err error
	if cfg.EngineTimeout, err = getDuration("LIKI_ENGINE_TIMEOUT_SECONDS", 30*time.Second); err != nil {
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
	if cfg.EngineMCPURL == "" {
		return Config{}, fmt.Errorf("LIKI_ENGINE_MCP_URL is required")
	}
	if cfg.Env != "development" && cfg.InternalToken == "" {
		return Config{}, fmt.Errorf("LIKI_AGENT_INTERNAL_TOKEN is required outside development")
	}
	if cfg.Env != "development" && cfg.LLMAPIKey == "" {
		return Config{}, fmt.Errorf("LIKI_LLM_API_KEY is required outside development")
	}
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(cfg.DataDir, "liki-agent-audit.db")
	}
	if cfg.LLMProvider == "" {
		// Keep explicit provider configuration available for production while
		// preserving zero-change behavior for existing Zhipu deployments.
		baseURL, err := url.Parse(cfg.LLMBaseURL)
		if err != nil {
			return Config{}, fmt.Errorf("LIKI_LLM_BASE_URL is invalid: %w", err)
		}
		host := strings.ToLower(baseURL.Hostname())
		if host == "open.bigmodel.cn" || strings.HasSuffix(host, ".open.bigmodel.cn") {
			cfg.LLMProvider = "zhipu"
		} else {
			cfg.LLMProvider = "openai-compatible"
		}
	}
	switch cfg.LLMProvider {
	case "openai-compatible", "openai", "zhipu", "bigmodel", "glm":
	default:
		return Config{}, fmt.Errorf("LIKI_LLM_PROVIDER is unsupported: %q", cfg.LLMProvider)
	}
	if cfg.RunTimeout <= 0 {
		return Config{}, fmt.Errorf("LIKI_RUN_TIMEOUT_SECONDS must be greater than zero")
	}
	if _, err := url.Parse(cfg.PublicURL); err != nil {
		return Config{}, fmt.Errorf("LIKI_AGENT_PUBLIC_URL is invalid: %w", err)
	}
	if cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("LIKI_SHUTDOWN_TIMEOUT_SECONDS must be greater than zero")
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

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}
