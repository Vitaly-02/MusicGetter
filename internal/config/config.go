// Package config loads and validates process configuration without logging secrets.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr          string
	LogLevel          slog.Level
	Database          Database
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	RequestTimeout    time.Duration
	HealthTimeout     time.Duration
	ShutdownTimeout   time.Duration
	MigrationTimeout  time.Duration
}

type Database struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	ConnectTimeout  time.Duration
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// LogValue makes accidental structured logging of Database safe.
func (d Database) LogValue() slog.Value {
	return slog.GroupValue(slog.Int("max_conns", int(d.MaxConns)), slog.Int("min_conns", int(d.MinConns)))
}

func Load() (Config, error) { return load(os.LookupEnv) }

func load(lookup func(string) (string, bool)) (Config, error) {
	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return value
		}
		return fallback
	}
	c := Config{HTTPAddr: get("HTTP_ADDR", ":8080")}
	_, port, err := net.SplitHostPort(c.HTTPAddr)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || n < 1 || n > 65535 {
		return Config{}, fmt.Errorf("HTTP_ADDR must be host:port with port 1..65535")
	}
	switch strings.ToLower(get("LOG_LEVEL", "info")) {
	case "debug":
		c.LogLevel = slog.LevelDebug
	case "info":
		c.LogLevel = slog.LevelInfo
	case "warn":
		c.LogLevel = slog.LevelWarn
	case "error":
		c.LogLevel = slog.LevelError
	default:
		return Config{}, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error")
	}
	c.Database.URL = get("DATABASE_URL", "")
	if strings.TrimSpace(c.Database.URL) == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	for _, field := range []struct {
		key, fallback string
		dest          *int32
		min           int64
	}{
		{"DB_MAX_CONNS", "10", &c.Database.MaxConns, 1},
		{"DB_MIN_CONNS", "0", &c.Database.MinConns, 0},
	} {
		n, err := strconv.ParseInt(get(field.key, field.fallback), 10, 32)
		if err != nil || n < field.min {
			return Config{}, fmt.Errorf("%s is outside the allowed integer range", field.key)
		}
		*field.dest = int32(n)
	}
	if c.Database.MinConns > c.Database.MaxConns {
		return Config{}, fmt.Errorf("DB_MIN_CONNS must not exceed DB_MAX_CONNS")
	}
	for _, field := range []struct {
		key, fallback string
		dest          *time.Duration
	}{
		{"DB_CONNECT_TIMEOUT", "5s", &c.Database.ConnectTimeout},
		{"DB_MAX_CONN_LIFETIME", "1h", &c.Database.MaxConnLifetime},
		{"DB_MAX_CONN_IDLE_TIME", "5m", &c.Database.MaxConnIdleTime},
		{"HTTP_READ_HEADER_TIMEOUT", "5s", &c.ReadHeaderTimeout},
		{"HTTP_READ_TIMEOUT", "15s", &c.ReadTimeout},
		{"HTTP_WRITE_TIMEOUT", "15s", &c.WriteTimeout},
		{"HTTP_IDLE_TIMEOUT", "60s", &c.IdleTimeout},
		{"HTTP_REQUEST_TIMEOUT", "10s", &c.RequestTimeout},
		{"HEALTH_TIMEOUT", "2s", &c.HealthTimeout},
		{"SHUTDOWN_TIMEOUT", "10s", &c.ShutdownTimeout},
		{"MIGRATION_TIMEOUT", "60s", &c.MigrationTimeout},
	} {
		d, err := time.ParseDuration(get(field.key, field.fallback))
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("%s must be a positive duration", field.key)
		}
		*field.dest = d
	}
	if c.HealthTimeout > c.RequestTimeout || c.RequestTimeout >= c.WriteTimeout {
		return Config{}, fmt.Errorf("timeouts must satisfy HEALTH_TIMEOUT <= HTTP_REQUEST_TIMEOUT < HTTP_WRITE_TIMEOUT")
	}
	return c, nil
}
