// Package health checks SOCKS5 availability and updates backend health state.
package health

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/rasooll/egressshuffle/internal/backend"
)

type Checker interface {
	Check(context.Context, string) error
}

// SOCKSChecker validates both TCP connectivity and the SOCKS5 greeting.
type SOCKSChecker struct {
	Dialer net.Dialer
}

func (c SOCKSChecker) Check(ctx context.Context, address string) error {
	conn, err := c.Dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect to SOCKS5 endpoint: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("set SOCKS5 health deadline: %w", err)
		}
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return fmt.Errorf("write SOCKS5 greeting: %w", err)
	}
	response := make([]byte, 2)
	if _, err := io.ReadFull(conn, response); err != nil {
		return fmt.Errorf("read SOCKS5 greeting: %w", err)
	}
	if response[0] != 0x05 || response[1] != 0x00 {
		return fmt.Errorf("SOCKS5 endpoint rejected unauthenticated greeting")
	}
	return nil
}

type Observer interface {
	HealthCheck(backendID string, err error, state backend.HealthState, changed bool)
}

// Manager periodically checks every discovered backend.
type Manager struct {
	Registry         *backend.Registry
	Checker          Checker
	Interval         time.Duration
	Timeout          time.Duration
	FailureThreshold int
	SuccessThreshold int
	Observer         Observer
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
	var wg sync.WaitGroup
	for _, item := range m.Registry.Snapshot() {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, m.Timeout)
			err := m.Checker.Check(checkCtx, item.Address())
			cancel()
			state, changed := item.ObserveHealth(err == nil, m.FailureThreshold, m.SuccessThreshold, time.Now())
			if m.Observer != nil {
				m.Observer.HealthCheck(item.ID(), err, state, changed)
			}
		}()
	}
	wg.Wait()
}
