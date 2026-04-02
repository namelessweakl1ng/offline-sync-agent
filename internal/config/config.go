package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type ClientConfig struct {
	AuthToken          string
	ServerURL          string
	DBPath             string
	LogLevel           slog.Level
	SyncInterval       time.Duration
	MaxBackoff         time.Duration
	HTTPTimeout        time.Duration
	InsecureSkipVerify bool
}

type ServerConfig struct {
	AuthToken           string
	Port                string
	LogLevel            slog.Level
	ShutdownTimeout     time.Duration
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	IdleTimeout         time.Duration
	MaxRequestBodyBytes int64
	RateLimitPerMinute  int
	StoreBackend        string
}

func LoadClientConfig() (ClientConfig, error) {
	level, err := parseLogLevel(getEnv("LOG_LEVEL", "INFO"))
	if err != nil {
		return ClientConfig{}, err
	}

	return ClientConfig{
		AuthToken:          strings.TrimSpace(os.Getenv("AUTH_TOKEN")),
		ServerURL:          strings.TrimRight(strings.TrimSpace(os.Getenv("SERVER_URL")), "/"),
		DBPath:             getEnv("SYNC_DB_PATH", "data.db"),
		LogLevel:           level,
		SyncInterval:       getDurationEnv("SYNC_INTERVAL", 5*time.Second),
		MaxBackoff:         getDurationEnv("SYNC_MAX_BACKOFF", 30*time.Second),
		HTTPTimeout:        getDurationEnv("HTTP_TIMEOUT", 10*time.Second),
		InsecureSkipVerify: getBoolEnv("INSECURE_SKIP_VERIFY", false),
	}, nil
}

func (c ClientConfig) ValidateSync() error {
	switch {
	case c.ServerURL == "":
		return fmt.Errorf("SERVER_URL is required for sync commands")
	case c.AuthToken == "":
		return fmt.Errorf("AUTH_TOKEN is required for sync commands")
	default:
		return nil
	}
}

func LoadServerConfig() (ServerConfig, error) {
	level, err := parseLogLevel(getEnv("LOG_LEVEL", "INFO"))
	if err != nil {
		return ServerConfig{}, err
	}

	return ServerConfig{
		AuthToken:           strings.TrimSpace(os.Getenv("AUTH_TOKEN")),
		Port:                getEnv("PORT", "8080"),
		LogLevel:            level,
		ShutdownTimeout:     getDurationEnv("SHUTDOWN_TIMEOUT", 10*time.Second),
		ReadTimeout:         getDurationEnv("HTTP_READ_TIMEOUT", 10*time.Second),
		WriteTimeout:        getDurationEnv("HTTP_WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:         getDurationEnv("HTTP_IDLE_TIMEOUT", 60*time.Second),
		MaxRequestBodyBytes: getInt64Env("MAX_REQUEST_BODY_BYTES", 1<<20),
		RateLimitPerMinute:  getIntEnv("RATE_LIMIT_PER_MINUTE", 1000),
		StoreBackend:        strings.ToLower(getEnv("BACKEND_STORE", "memory")),
	}, nil
}

func (c ServerConfig) Validate() error {
	if c.AuthToken == "" {
		return fmt.Errorf("AUTH_TOKEN is required")
	}

	if c.Port == "" {
		return fmt.Errorf("PORT cannot be empty")
	}

	return nil
}

func getEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func getIntEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func getInt64Env(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}

	return parsed
}

func getBoolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "DEBUG":
		return slog.LevelDebug, nil
	case "", "INFO":
		return slog.LevelInfo, nil
	case "WARN", "WARNING":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unsupported LOG_LEVEL %q", raw)
	}
}
