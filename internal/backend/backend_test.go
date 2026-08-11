package backend

import (
	"testing"
	"time"
)

func TestRegistryReconcile(t *testing.T) {
	r := NewRegistry()
	added, removed := r.Reconcile([]string{"10.0.0.2:9050", "10.0.0.1:9050", "10.0.0.2:9050"})
	if added != 2 || removed != 0 {
		t.Fatalf("first reconcile = added %d removed %d", added, removed)
	}
	items := r.Snapshot()
	if len(items) != 2 || items[0].Address() != "10.0.0.1:9050" {
		t.Fatalf("unexpected snapshot: %+v", items)
	}

	preserved := items[0]
	preserved.ObserveHealth(true, 3, 1, time.Now())
	added, removed = r.Reconcile([]string{"10.0.0.1:9050", "10.0.0.3:9050"})
	if added != 1 || removed != 1 {
		t.Fatalf("second reconcile = added %d removed %d", added, removed)
	}
	items = r.Snapshot()
	if items[0] != preserved || !items[0].Healthy() {
		t.Fatal("existing backend runtime state was not preserved")
	}
}

func TestBackendHealthTransitions(t *testing.T) {
	b := New("127.0.0.1:9050")
	now := time.Now()

	state, changed := b.ObserveHealth(true, 2, 2, now)
	if state != HealthUnknown || changed {
		t.Fatalf("first success = %s, changed %v", state, changed)
	}
	state, changed = b.ObserveHealth(true, 2, 2, now)
	if state != HealthHealthy || !changed {
		t.Fatalf("second success = %s, changed %v", state, changed)
	}
	b.ObserveHealth(false, 2, 2, now)
	if !b.Healthy() {
		t.Fatal("single failure should not mark backend unhealthy")
	}
	state, changed = b.ObserveHealth(false, 2, 2, now)
	if state != HealthUnhealthy || !changed {
		t.Fatalf("second failure = %s, changed %v", state, changed)
	}
	b.ObserveHealth(true, 2, 2, now)
	state, changed = b.ObserveHealth(true, 2, 2, now)
	if state != HealthHealthy || !changed {
		t.Fatalf("recovery = %s, changed %v", state, changed)
	}
}

func TestBeginConnectionReleaseIsIdempotent(t *testing.T) {
	b := New("127.0.0.1:9050")
	release := b.BeginConnection()
	if b.Active() != 1 {
		t.Fatalf("active = %d, want 1", b.Active())
	}
	release()
	release()
	if b.Active() != 0 {
		t.Fatalf("active = %d, want 0", b.Active())
	}
}
