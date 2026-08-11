// Package discovery periodically resolves Tor service endpoints.
package discovery

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/rasooll/egressshuffle/internal/backend"
)

type Resolver interface {
	LookupHost(context.Context, string) ([]string, error)
}

// DNS discovers backend addresses with Docker's internal DNS resolver.
type DNS struct {
	Resolver Resolver
	Service  string
	Port     uint16
}

func (d DNS) Discover(ctx context.Context) ([]string, error) {
	addresses, err := d.Resolver.LookupHost(ctx, d.Service)
	if err != nil {
		return nil, fmt.Errorf("resolve Tor service %q: %w", d.Service, err)
	}
	unique := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		ip := net.ParseIP(address)
		if ip == nil {
			continue
		}
		unique[net.JoinHostPort(ip.String(), strconv.Itoa(int(d.Port)))] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for address := range unique {
		result = append(result, address)
	}
	sort.Strings(result)
	return result, nil
}

type Observer interface {
	DiscoveryRun(err error)
}

// Manager owns the periodic discovery and reconciliation loop.
type Manager struct {
	Discoverer interface {
		Discover(context.Context) ([]string, error)
	}
	Registry *backend.Registry
	Interval time.Duration
	Observer Observer
}

func (m *Manager) Run(ctx context.Context) {
	m.runOnce(ctx)
	ticker := time.NewTicker(m.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runOnce(ctx)
		}
	}
}

func (m *Manager) runOnce(ctx context.Context) {
	addresses, err := m.Discoverer.Discover(ctx)
	if m.Observer != nil {
		m.Observer.DiscoveryRun(err)
	}
	if err != nil {
		return // A transient lookup failure must retain the last known membership.
	}
	m.Registry.Reconcile(addresses)
}
