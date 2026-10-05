package config

import (
	"strings"
	"testing"
	"time"
)

func env(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { v, ok := values[key]; return v, ok }
}

func TestLoadDefaultsAndOverrides(t *testing.T) {
	cfg, err := load(env(map[string]string{"DATABASE_URL": "postgres://localhost/test"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.Database.MaxConns != 10 || cfg.HealthTimeout != 2*time.Second {
		t.Fatalf("unexpected defaults: address=%s max=%d health=%s", cfg.HTTPAddr, cfg.Database.MaxConns, cfg.HealthTimeout)
	}
	cfg, err = load(env(map[string]string{"DATABASE_URL": "postgres://localhost/test", "HTTP_ADDR": "127.0.0.1:9000", "DB_MAX_CONNS": "3", "DB_MIN_CONNS": "2", "HEALTH_TIMEOUT": "500ms", "LOG_LEVEL": "DEBUG"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9000" || cfg.Database.MaxConns != 3 || cfg.Database.MinConns != 2 || cfg.HealthTimeout != 500*time.Millisecond || cfg.LogLevel.String() != "DEBUG" {
		t.Fatal("overrides not applied")
	}
}

func TestInvalidConfigurationDoesNotEchoValues(t *testing.T) {
	cases := []struct{ key, value string }{
		{"DATABASE_URL", ""}, {"HTTP_ADDR", "secret-value"}, {"HTTP_ADDR", ":0"},
		{"LOG_LEVEL", "secret-value"}, {"DB_MAX_CONNS", "0"}, {"DB_MAX_CONNS", "2147483648"},
		{"DB_MIN_CONNS", "-1"}, {"DB_MIN_CONNS", "11"},
		{"DB_CONNECT_TIMEOUT", "0s"}, {"DB_MAX_CONN_LIFETIME", "-1s"},
		{"HTTP_REQUEST_TIMEOUT", "secret-value"}, {"HTTP_WRITE_TIMEOUT", "10s"},
		{"HEALTH_TIMEOUT", "11s"}, {"SHUTDOWN_TIMEOUT", ""}, {"MIGRATION_TIMEOUT", "-1s"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			values := map[string]string{"DATABASE_URL": "postgres://user:secret-value@localhost/test"}
			values[tc.key] = tc.value
			_, err := load(env(values))
			if err == nil {
				t.Fatal("expected validation failure")
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatal("configuration error leaked value")
			}
		})
	}
}
