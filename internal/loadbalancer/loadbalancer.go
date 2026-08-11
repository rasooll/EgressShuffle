// Package loadbalancer selects healthy Tor backends.
package loadbalancer

import (
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"

	"github.com/rasooll/egressshuffle/internal/backend"
)

var ErrNoHealthyBackends = errors.New("no healthy Tor backends")

type LoadBalancer interface {
	Select([]*backend.Backend) (*backend.Backend, error)
}

func New(strategy string) (LoadBalancer, error) {
	switch strategy {
	case "round_robin":
		return &RoundRobin{}, nil
	case "random":
		return &Random{source: rand.New(rand.NewSource(1))}, nil
	case "least_connections":
		return LeastConnections{}, nil
	default:
		return nil, errors.New("unsupported load-balancing strategy")
	}
}

type RoundRobin struct {
	next atomic.Uint64
}

func (r *RoundRobin) Select(items []*backend.Backend) (*backend.Backend, error) {
	count := healthyCount(items)
	if count == 0 {
		return nil, ErrNoHealthyBackends
	}
	target := int((r.next.Add(1) - 1) % uint64(count))
	return nthHealthy(items, target), nil
}

type Random struct {
	mu     sync.Mutex
	source *rand.Rand
}

func (r *Random) Select(items []*backend.Backend) (*backend.Backend, error) {
	count := healthyCount(items)
	if count == 0 {
		return nil, ErrNoHealthyBackends
	}
	r.mu.Lock()
	target := r.source.Intn(count)
	r.mu.Unlock()
	return nthHealthy(items, target), nil
}

type LeastConnections struct{}

func (LeastConnections) Select(items []*backend.Backend) (*backend.Backend, error) {
	var selected *backend.Backend
	var minimum int64
	for _, item := range items {
		if !item.Healthy() {
			continue
		}
		active := item.Active()
		if selected == nil || active < minimum || (active == minimum && item.ID() < selected.ID()) {
			selected = item
			minimum = active
		}
	}
	if selected == nil {
		return nil, ErrNoHealthyBackends
	}
	return selected, nil
}

func healthyCount(items []*backend.Backend) int {
	count := 0
	for _, item := range items {
		if item.Healthy() {
			count++
		}
	}
	return count
}

func nthHealthy(items []*backend.Backend, target int) *backend.Backend {
	for _, item := range items {
		if item.Healthy() {
			if target == 0 {
				return item
			}
			target--
		}
	}
	return nil
}
