package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/rasooll/egressshuffle/internal/backend"
)

func TestRenderIncludesRequiredMetrics(t *testing.T) {
	registry := backend.NewRegistry()
	registry.Reconcile([]string{"127.0.0.1:9050"})
	item := registry.Snapshot()[0]
	item.ObserveHealth(true, 1, 1, time.Now())

	m := New(registry, "round_robin")
	m.Request("GET", "success", 25*time.Millisecond)
	m.BackendConnection(item.ID(), "success")
	m.DiscoveryRun(nil)
	m.HealthCheck(item.ID(), nil, backend.HealthHealthy, true)
	output := m.render()
	for _, name := range []string{
		"egressshuffle_requests_total", "egressshuffle_request_duration_seconds",
		"egressshuffle_active_connections", "egressshuffle_backend_count",
		"egressshuffle_healthy_backend_count", "egressshuffle_backend_active_connections",
		"egressshuffle_backend_failures_total", "egressshuffle_backend_connections_total",
		"egressshuffle_discovery_runs_total", "egressshuffle_discovery_errors_total",
		"egressshuffle_health_checks_total", "egressshuffle_health_check_failures_total",
	} {
		if !strings.Contains(output, name) {
			t.Errorf("metrics output does not contain %s", name)
		}
	}
	if strings.Contains(output, "127.0.0.1:9050") {
		t.Fatal("metrics exposed a backend address")
	}
}
