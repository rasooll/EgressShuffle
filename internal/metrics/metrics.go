// Package metrics exposes a small Prometheus-compatible metrics registry.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rasooll/egressshuffle/internal/backend"
)

var durationBuckets = [...]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

type requestKey struct {
	method string
	result string
}

type durationData struct {
	count   uint64
	sum     float64
	buckets [len(durationBuckets)]uint64
}

// Metrics implements observers for discovery, health, and proxy traffic.
type Metrics struct {
	registry     *backend.Registry
	loadBalancer string

	active          atomic.Int64
	discoveryRuns   atomic.Uint64
	discoveryErrors atomic.Uint64
	healthChecks    atomic.Uint64
	healthFailures  atomic.Uint64

	mu                 sync.Mutex
	requests           map[requestKey]uint64
	durations          map[requestKey]*durationData
	backendConnections map[string]uint64
	backendFailures    map[string]uint64
}

func New(registry *backend.Registry, loadBalancer string) *Metrics {
	return &Metrics{
		registry:           registry,
		loadBalancer:       loadBalancer,
		requests:           make(map[requestKey]uint64),
		durations:          make(map[requestKey]*durationData),
		backendConnections: make(map[string]uint64),
		backendFailures:    make(map[string]uint64),
	}
}

func (m *Metrics) Request(method, result string, duration time.Duration) {
	key := requestKey{method: boundedMethod(method), result: result}
	seconds := duration.Seconds()
	m.mu.Lock()
	m.requests[key]++
	data := m.durations[key]
	if data == nil {
		data = &durationData{}
		m.durations[key] = data
	}
	data.count++
	data.sum += seconds
	for i, bound := range durationBuckets {
		if seconds <= bound {
			data.buckets[i]++
		}
	}
	m.mu.Unlock()
}

func (m *Metrics) BackendConnection(backendID, result string) {
	key := backendID + "\x00" + result
	m.mu.Lock()
	m.backendConnections[key]++
	if result == "failure" {
		m.backendFailures[backendID]++
	}
	m.mu.Unlock()
}

func (m *Metrics) ActiveConnections(delta int64) { m.active.Add(delta) }

func (m *Metrics) DiscoveryRun(err error) {
	m.discoveryRuns.Add(1)
	if err != nil {
		m.discoveryErrors.Add(1)
	}
}

func (m *Metrics) HealthCheck(_ string, err error, _ backend.HealthState, _ bool) {
	m.healthChecks.Add(1)
	if err != nil {
		m.healthFailures.Add(1)
	}
}

func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, m.render())
}

func (m *Metrics) render() string {
	var b strings.Builder
	writeHelpType(&b, "egressshuffle_requests_total", "HTTP proxy requests by method and result.", "counter")
	writeHelpType(&b, "egressshuffle_request_duration_seconds", "HTTP proxy request duration.", "histogram")
	m.mu.Lock()
	requestKeys := make([]requestKey, 0, len(m.requests))
	for key := range m.requests {
		requestKeys = append(requestKeys, key)
	}
	sort.Slice(requestKeys, func(i, j int) bool {
		if requestKeys[i].method == requestKeys[j].method {
			return requestKeys[i].result < requestKeys[j].result
		}
		return requestKeys[i].method < requestKeys[j].method
	})
	for _, key := range requestKeys {
		labels := `method="` + escape(key.method) + `",result="` + escape(key.result) + `"`
		fmt.Fprintf(&b, "egressshuffle_requests_total{%s} %d\n", labels, m.requests[key])
		data := m.durations[key]
		for i, bound := range durationBuckets {
			fmt.Fprintf(&b, "egressshuffle_request_duration_seconds_bucket{%s,le=%q} %d\n", labels, strconv.FormatFloat(bound, 'g', -1, 64), data.buckets[i])
		}
		fmt.Fprintf(&b, "egressshuffle_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels, data.count)
		fmt.Fprintf(&b, "egressshuffle_request_duration_seconds_sum{%s} %g\n", labels, data.sum)
		fmt.Fprintf(&b, "egressshuffle_request_duration_seconds_count{%s} %d\n", labels, data.count)
	}
	connections := cloneMap(m.backendConnections)
	failures := cloneMap(m.backendFailures)
	m.mu.Unlock()

	writeGauge(&b, "egressshuffle_active_connections", "Current active proxy connections.", m.active.Load())
	total, healthy := m.registry.Counts()
	writeGauge(&b, "egressshuffle_backend_count", "Current discovered Tor backends.", int64(total))
	writeGauge(&b, "egressshuffle_healthy_backend_count", "Current healthy Tor backends.", int64(healthy))
	writeHelpType(&b, "egressshuffle_backend_active_connections", "Current active connections by opaque backend ID.", "gauge")
	for _, item := range m.registry.Snapshot() {
		fmt.Fprintf(&b, "egressshuffle_backend_active_connections{backend_id=%q} %d\n", escape(item.ID()), item.Active())
	}

	writeHelpType(&b, "egressshuffle_backend_connections_total", "Backend connection attempts.", "counter")
	for _, key := range sortedKeys(connections) {
		parts := strings.SplitN(key, "\x00", 2)
		fmt.Fprintf(&b, "egressshuffle_backend_connections_total{backend_id=%q,result=%q} %d\n", escape(parts[0]), escape(parts[1]), connections[key])
	}
	writeHelpType(&b, "egressshuffle_backend_failures_total", "Failed backend connection attempts.", "counter")
	for _, id := range sortedKeys(failures) {
		fmt.Fprintf(&b, "egressshuffle_backend_failures_total{backend_id=%q} %d\n", escape(id), failures[id])
	}

	writeCounter(&b, "egressshuffle_discovery_runs_total", "DNS discovery runs.", m.discoveryRuns.Load())
	writeCounter(&b, "egressshuffle_discovery_errors_total", "Failed DNS discovery runs.", m.discoveryErrors.Load())
	writeCounter(&b, "egressshuffle_health_checks_total", "Backend health checks.", m.healthChecks.Load())
	writeCounter(&b, "egressshuffle_health_check_failures_total", "Failed backend health checks.", m.healthFailures.Load())
	writeHelpType(&b, "egressshuffle_load_balancer_info", "Configured load-balancing strategy.", "gauge")
	fmt.Fprintf(&b, "egressshuffle_load_balancer_info{load_balancer=%q} 1\n", escape(m.loadBalancer))
	return b.String()
}

func boundedMethod(method string) string {
	switch method {
	case "CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT":
		return method
	default:
		return "OTHER"
	}
}

func writeHelpType(b *strings.Builder, name, help, metricType string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
}

func writeGauge(b *strings.Builder, name, help string, value int64) {
	writeHelpType(b, name, help, "gauge")
	fmt.Fprintf(b, "%s %d\n", name, value)
}

func writeCounter(b *strings.Builder, name, help string, value uint64) {
	writeHelpType(b, name, help, "counter")
	fmt.Fprintf(b, "%s %d\n", name, value)
}

func escape(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func cloneMap(source map[string]uint64) map[string]uint64 {
	result := make(map[string]uint64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func sortedKeys(values map[string]uint64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
