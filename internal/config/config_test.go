package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, name := range []string{
		"PROXY_ADDRESS", "ADMIN_ADDRESS", "TOR_SERVICE_NAME", "TOR_SOCKS_PORT",
		"DISCOVERY_INTERVAL", "LOAD_BALANCER", "CONNECT_TIMEOUT", "REQUEST_TIMEOUT",
		"IDLE_TIMEOUT", "HEADER_TIMEOUT", "SHUTDOWN_TIMEOUT", "BACKEND_HEALTH_INTERVAL",
		"BACKEND_HEALTH_TIMEOUT", "BACKEND_FAILURE_THRESHOLD", "BACKEND_SUCCESS_THRESHOLD",
		"MAX_BACKEND_RETRIES", "TOR_E2E_HEALTHCHECK_ENABLED", "TOR_E2E_HEALTHCHECK_URL",
		"LOG_LEVEL", "LOG_FORMAT", "PROXY_AUTH_ENABLED", "PROXY_AUTH_USERNAME", "PROXY_AUTH_PASSWORD",
	} {
		t.Setenv(name, "")
	}
	// Empty strings are explicit values, so restore variables that use non-empty defaults.
	t.Setenv("PROXY_ADDRESS", "127.0.0.1:8080")
	t.Setenv("ADMIN_ADDRESS", "127.0.0.1:9090")
	t.Setenv("TOR_SERVICE_NAME", "tor")
	t.Setenv("TOR_SOCKS_PORT", "9050")
	t.Setenv("DISCOVERY_INTERVAL", "10s")
	t.Setenv("LOAD_BALANCER", LoadBalancerRoundRobin)
	t.Setenv("CONNECT_TIMEOUT", "15s")
	t.Setenv("REQUEST_TIMEOUT", "1m")
	t.Setenv("IDLE_TIMEOUT", "90s")
	t.Setenv("HEADER_TIMEOUT", "10s")
	t.Setenv("SHUTDOWN_TIMEOUT", "15s")
	t.Setenv("BACKEND_HEALTH_INTERVAL", "10s")
	t.Setenv("BACKEND_HEALTH_TIMEOUT", "5s")
	t.Setenv("BACKEND_FAILURE_THRESHOLD", "3")
	t.Setenv("BACKEND_SUCCESS_THRESHOLD", "2")
	t.Setenv("MAX_BACKEND_RETRIES", "2")
	t.Setenv("TOR_E2E_HEALTHCHECK_ENABLED", "false")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("PROXY_AUTH_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ProxyAddress != "127.0.0.1:8080" || cfg.TorSOCKSPort != 9050 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.DiscoveryInterval != 10*time.Second || cfg.MaxBackendRetries != 2 {
		t.Fatalf("unexpected timing/retry defaults: %+v", cfg)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		value string
	}{
		{"invalid duration", "CONNECT_TIMEOUT", "eventually"},
		{"negative duration", "DISCOVERY_INTERVAL", "-1s"},
		{"invalid balancer", "LOAD_BALANCER", "first"},
		{"zero threshold", "BACKEND_FAILURE_THRESHOLD", "0"},
		{"invalid address", "PROXY_ADDRESS", "8080"},
		{"invalid log format", "LOG_FORMAT", "yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.env, tt.value)
			if _, err := Load(); err == nil {
				t.Fatal("Load() expected an error")
			}
		})
	}
}

func TestLoadValidatesAuthentication(t *testing.T) {
	t.Setenv("PROXY_AUTH_ENABLED", "true")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected missing credentials error")
	}

	t.Setenv("PROXY_AUTH_USERNAME", "proxy-user")
	t.Setenv("PROXY_AUTH_PASSWORD", "proxy-password")
	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadValidatesE2EHealthcheck(t *testing.T) {
	t.Setenv("TOR_E2E_HEALTHCHECK_ENABLED", "true")
	t.Setenv("TOR_E2E_HEALTHCHECK_URL", "file:///etc/passwd")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected invalid URL error")
	}

	t.Setenv("TOR_E2E_HEALTHCHECK_URL", "https://check.example/status")
	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}
