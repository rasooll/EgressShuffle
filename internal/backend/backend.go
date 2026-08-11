// Package backend owns the concurrency-safe Tor backend state and registry.
package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// HealthState is the backend's health manager state.
type HealthState uint8

const (
	HealthUnknown HealthState = iota
	HealthHealthy
	HealthUnhealthy
)

func (s HealthState) String() string {
	switch s {
	case HealthHealthy:
		return "healthy"
	case HealthUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// Backend represents one discovered Tor SOCKS5 endpoint.
type Backend struct {
	id      string
	address string
	active  atomic.Int64

	mu              sync.RWMutex
	health          HealthState
	lastHealthCheck time.Time
	consecutiveFail int
	consecutiveOK   int
}

// New creates a backend in the unknown health state.
func New(address string) *Backend {
	sum := sha256.Sum256([]byte(address))
	return &Backend{id: "tor-" + hex.EncodeToString(sum[:6]), address: address}
}

func (b *Backend) ID() string      { return b.id }
func (b *Backend) Address() string { return b.address }
func (b *Backend) Active() int64   { return b.active.Load() }

// Healthy reports whether the backend may receive proxy traffic.
func (b *Backend) Healthy() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.health == HealthHealthy
}

// State returns a consistent snapshot of runtime backend state.
func (b *Backend) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return State{
		ID:              b.id,
		Health:          b.health,
		Active:          b.active.Load(),
		LastHealthCheck: b.lastHealthCheck,
		ConsecutiveFail: b.consecutiveFail,
		ConsecutiveOK:   b.consecutiveOK,
	}
}

// ObserveHealth records one health result and applies transition thresholds.
func (b *Backend) ObserveHealth(ok bool, failureThreshold, successThreshold int, now time.Time) (HealthState, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	previous := b.health
	b.lastHealthCheck = now
	if ok {
		b.consecutiveOK++
		b.consecutiveFail = 0
		if b.health != HealthHealthy && b.consecutiveOK >= successThreshold {
			b.health = HealthHealthy
		}
	} else {
		b.consecutiveFail++
		b.consecutiveOK = 0
		if b.health == HealthHealthy && b.consecutiveFail >= failureThreshold {
			b.health = HealthUnhealthy
		} else if b.health == HealthUnknown && b.consecutiveFail >= failureThreshold {
			b.health = HealthUnhealthy
		}
	}
	return b.health, previous != b.health
}

// BeginConnection increments the active count and returns an idempotent release function.
func (b *Backend) BeginConnection() func() {
	b.active.Add(1)
	var once sync.Once
	return func() { once.Do(func() { b.active.Add(-1) }) }
}

// State is a non-sensitive backend runtime snapshot.
type State struct {
	ID              string
	Health          HealthState
	Active          int64
	LastHealthCheck time.Time
	ConsecutiveFail int
	ConsecutiveOK   int
}

// Registry reconciles discovered addresses while preserving runtime state.
type Registry struct {
	mu                sync.RWMutex
	backends          map[string]*Backend
	discoveryComplete bool
}

func NewRegistry() *Registry {
	return &Registry{backends: make(map[string]*Backend)}
}

// Reconcile atomically replaces membership and preserves existing backends.
func (r *Registry) Reconcile(addresses []string) (added, removed int) {
	next := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		if address != "" {
			next[address] = struct{}{}
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for address := range next {
		if _, exists := r.backends[address]; !exists {
			r.backends[address] = New(address)
			added++
		}
	}
	for address := range r.backends {
		if _, exists := next[address]; !exists {
			delete(r.backends, address)
			removed++
		}
	}
	r.discoveryComplete = true
	return added, removed
}

// Snapshot returns backends in deterministic address order.
func (r *Registry) Snapshot() []*Backend {
	r.mu.RLock()
	result := make([]*Backend, 0, len(r.backends))
	for _, item := range r.backends {
		result = append(result, item)
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].address < result[j].address })
	return result
}

func (r *Registry) Counts() (total, healthy int) {
	items := r.Snapshot()
	for _, item := range items {
		if item.Healthy() {
			healthy++
		}
	}
	return len(items), healthy
}

func (r *Registry) DiscoveryComplete() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.discoveryComplete
}
