package loadbalancer

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/rasooll/egressshuffle/internal/backend"
)

func healthy(address string) *backend.Backend {
	b := backend.New(address)
	b.ObserveHealth(true, 1, 1, time.Now())
	return b
}

func TestRoundRobin(t *testing.T) {
	a, b := healthy("a:9050"), healthy("b:9050")
	r := &RoundRobin{}
	for i, want := range []*backend.Backend{a, b, a, b} {
		got, err := r.Select([]*backend.Backend{a, b})
		if err != nil || got != want {
			t.Fatalf("selection %d = %v, %v; want %v", i, got, err, want)
		}
	}
}

func TestLeastConnections(t *testing.T) {
	a, b := healthy("a:9050"), healthy("b:9050")
	release := a.BeginConnection()
	defer release()
	got, err := (LeastConnections{}).Select([]*backend.Backend{a, b})
	if err != nil || got != b {
		t.Fatalf("Select() = %v, %v; want less busy backend", got, err)
	}
}

func TestRandomSelectsHealthyBackends(t *testing.T) {
	healthyBackend := healthy("healthy:9050")
	unhealthy := backend.New("unknown:9050")
	r, _ := New("random")
	for range 20 {
		got, err := r.Select([]*backend.Backend{unhealthy, healthyBackend})
		if err != nil || got != healthyBackend {
			t.Fatalf("Select() = %v, %v", got, err)
		}
	}
}

func TestNoHealthyBackends(t *testing.T) {
	for _, lb := range []LoadBalancer{&RoundRobin{}, &Random{source: randSource()}, LeastConnections{}} {
		if _, err := lb.Select([]*backend.Backend{backend.New("a:9050")}); err != ErrNoHealthyBackends {
			t.Fatalf("Select() error = %v, want %v", err, ErrNoHealthyBackends)
		}
	}
}

func TestConcurrentRoundRobinSelection(t *testing.T) {
	items := []*backend.Backend{healthy("a:9050"), healthy("b:9050")}
	balancers := []LoadBalancer{&RoundRobin{}, &Random{source: randSource()}, LeastConnections{}}
	for _, balancer := range balancers {
		var wg sync.WaitGroup
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := balancer.Select(items); err != nil {
					t.Errorf("Select() error = %v", err)
				}
			}()
		}
		wg.Wait()
	}
}

func TestSelectionDuringHealthTransitions(t *testing.T) {
	a, b := healthy("a:9050"), healthy("b:9050")
	items := []*backend.Backend{a, b}
	balancers := []LoadBalancer{&RoundRobin{}, &Random{source: randSource()}}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 2_000 {
			a.ObserveHealth(false, 1, 1, time.Now())
			b.ObserveHealth(false, 1, 1, time.Now())
			a.ObserveHealth(true, 1, 1, time.Now())
			b.ObserveHealth(true, 1, 1, time.Now())
		}
	}()
	for _, balancer := range balancers {
		for range 2_000 {
			selected, err := balancer.Select(items)
			if err == nil && selected == nil {
				t.Fatal("Select() returned a nil backend without an error")
			}
		}
	}
	wg.Wait()
}

func randSource() *rand.Rand {
	return rand.New(rand.NewSource(1))
}
