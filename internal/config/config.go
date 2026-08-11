// Package config loads and validates EgressShuffle's environment configuration.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	LoadBalancerRoundRobin       = "round_robin"
	LoadBalancerRandom           = "random"
	LoadBalancerLeastConnections = "least_connections"
)

// Config contains all runtime configuration.
type Config struct {
	ProxyAddress   string
	AdminAddress   string
	TorServiceName string
	TorSOCKSPort   uint16

	DiscoveryInterval time.Duration
	LoadBalancer      string
	ConnectTimeout    time.Duration
	RequestTimeout    time.Duration
	IdleTimeout       time.Duration
	HeaderTimeout     time.Duration
	ShutdownTimeout   time.Duration

	BackendHealthInterval   time.Duration
	BackendHealthTimeout    time.Duration
	BackendFailureThreshold int
	BackendSuccessThreshold int
	MaxBackendRetries       int

	TorE2EHealthcheckEnabled bool
	TorE2EHealthcheckURL     string

	LogLevel  string
	LogFormat string

	ProxyAuthEnabled  bool
	ProxyAuthUsername string
	ProxyAuthPassword string
}

// Load reads configuration from environment variables and validates it.
func Load() (Config, error) {
	cfg := Config{
		ProxyAddress:         env("PROXY_ADDRESS", "127.0.0.1:8080"),
		AdminAddress:         env("ADMIN_ADDRESS", "127.0.0.1:9090"),
		TorServiceName:       env("TOR_SERVICE_NAME", "tor"),
		LoadBalancer:         env("LOAD_BALANCER", LoadBalancerRoundRobin),
		TorE2EHealthcheckURL: os.Getenv("TOR_E2E_HEALTHCHECK_URL"),
		LogLevel:             env("LOG_LEVEL", "info"),
		LogFormat:            env("LOG_FORMAT", "json"),
		ProxyAuthUsername:    os.Getenv("PROXY_AUTH_USERNAME"),
		ProxyAuthPassword:    os.Getenv("PROXY_AUTH_PASSWORD"),
	}

	var err error
	if cfg.TorSOCKSPort, err = parsePort("TOR_SOCKS_PORT", 9050); err != nil {
		return Config{}, err
	}

	durations := []struct {
		name string
		dst  *time.Duration
		def  time.Duration
	}{
		{"DISCOVERY_INTERVAL", &cfg.DiscoveryInterval, 10 * time.Second},
		{"CONNECT_TIMEOUT", &cfg.ConnectTimeout, 15 * time.Second},
		{"REQUEST_TIMEOUT", &cfg.RequestTimeout, 60 * time.Second},
		{"IDLE_TIMEOUT", &cfg.IdleTimeout, 90 * time.Second},
		{"HEADER_TIMEOUT", &cfg.HeaderTimeout, 10 * time.Second},
		{"SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout, 15 * time.Second},
		{"BACKEND_HEALTH_INTERVAL", &cfg.BackendHealthInterval, 10 * time.Second},
		{"BACKEND_HEALTH_TIMEOUT", &cfg.BackendHealthTimeout, 5 * time.Second},
	}
	for _, item := range durations {
		if *item.dst, err = parseDuration(item.name, item.def); err != nil {
			return Config{}, err
		}
	}

	integers := []struct {
		name string
		dst  *int
		def  int
		min  int
	}{
		{"BACKEND_FAILURE_THRESHOLD", &cfg.BackendFailureThreshold, 3, 1},
		{"BACKEND_SUCCESS_THRESHOLD", &cfg.BackendSuccessThreshold, 2, 1},
		{"MAX_BACKEND_RETRIES", &cfg.MaxBackendRetries, 2, 0},
	}
	for _, item := range integers {
		if *item.dst, err = parseInt(item.name, item.def, item.min); err != nil {
			return Config{}, err
		}
	}

	if cfg.TorE2EHealthcheckEnabled, err = parseBool("TOR_E2E_HEALTHCHECK_ENABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.ProxyAuthEnabled, err = parseBool("PROXY_AUTH_ENABLED", false); err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks configuration invariants and returns actionable errors.
func (c Config) Validate() error {
	if err := validateListenAddress("PROXY_ADDRESS", c.ProxyAddress); err != nil {
		return err
	}
	if err := validateListenAddress("ADMIN_ADDRESS", c.AdminAddress); err != nil {
		return err
	}
	if strings.TrimSpace(c.TorServiceName) == "" {
		return fmt.Errorf("TOR_SERVICE_NAME must not be empty")
	}
	switch c.LoadBalancer {
	case LoadBalancerRoundRobin, LoadBalancerRandom, LoadBalancerLeastConnections:
	default:
		return fmt.Errorf("LOAD_BALANCER %q is unsupported", c.LoadBalancer)
	}
	if c.ProxyAuthEnabled && (c.ProxyAuthUsername == "" || c.ProxyAuthPassword == "") {
		return fmt.Errorf("PROXY_AUTH_USERNAME and PROXY_AUTH_PASSWORD are required when PROXY_AUTH_ENABLED=true")
	}
	if c.TorE2EHealthcheckEnabled {
		u, err := url.ParseRequestURI(c.TorE2EHealthcheckURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("TOR_E2E_HEALTHCHECK_URL must be a valid HTTP or HTTPS URL when enabled")
		}
	}
	if c.LogLevel != "debug" && c.LogLevel != "info" && c.LogLevel != "warn" && c.LogLevel != "error" {
		return fmt.Errorf("LOG_LEVEL %q is unsupported", c.LogLevel)
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		return fmt.Errorf("LOG_FORMAT %q is unsupported", c.LogFormat)
	}
	return nil
}

func env(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}

func parseDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := env(name, fallback.String())
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration, got %q", name, value)
	}
	return d, nil
}

func parseInt(name string, fallback, minimum int) (int, error) {
	value := env(name, strconv.Itoa(fallback))
	n, err := strconv.Atoi(value)
	if err != nil || n < minimum {
		return 0, fmt.Errorf("%s must be an integer greater than or equal to %d, got %q", name, minimum, value)
	}
	return n, nil
}

func parsePort(name string, fallback uint16) (uint16, error) {
	n, err := parseInt(name, int(fallback), 1)
	if err != nil || n > 65535 {
		return 0, fmt.Errorf("%s must be a port between 1 and 65535", name)
	}
	return uint16(n), nil
}

func parseBool(name string, fallback bool) (bool, error) {
	value := env(name, strconv.FormatBool(fallback))
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean, got %q", name, value)
	}
	return b, nil
}

func validateListenAddress(name, address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%s must be a valid host:port address: %w", name, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%s has invalid port %q", name, port)
	}
	return nil
}
