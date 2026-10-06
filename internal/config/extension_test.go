package config

import "testing"

func TestExtensionOriginsAndLimits(t *testing.T) {
	for _, origin := range []string{"*", "https://example.com", "null", "chrome-extension://abc", "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/", "moz-extension://bad"} {
		if _, err := load(env(map[string]string{"DATABASE_URL": "postgres://localhost/test", "EXTENSION_ORIGINS": origin})); err == nil {
			t.Fatal("unsafe origin allowed")
		}
	}
	cfg, err := load(env(map[string]string{"DATABASE_URL": "postgres://localhost/test", "EXTENSION_ORIGINS": "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,moz-extension://00000000-0000-0000-0000-000000000001", "API_OWNER_PER_MINUTE": "20"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ExtensionOrigins) != 2 || cfg.APIOwnerPerMinute != 20 || cfg.APIClaimPerMinute != 5 {
		t.Fatal("wrong defaults")
	}
	for _, value := range []string{"0", "-1", "secret", "10001"} {
		if _, err = load(env(map[string]string{"DATABASE_URL": "postgres://localhost/test", "API_IP_PER_MINUTE": value})); err == nil {
			t.Fatal("invalid rate accepted")
		}
	}
}
