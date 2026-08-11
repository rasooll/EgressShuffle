package health

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rasooll/egressshuffle/internal/backend"
)

type checkerFunc func(context.Context, string) error

func (f checkerFunc) Check(ctx context.Context, address string) error { return f(ctx, address) }

func TestManagerAppliesThresholds(t *testing.T) {
	r := backend.NewRegistry()
	r.Reconcile([]string{"127.0.0.1:9050"})
	fail := false
	m := Manager{
		Registry: r,
		Checker: checkerFunc(func(context.Context, string) error {
			if fail {
				return errors.New("unavailable")
			}
			return nil
		}),
		Timeout:          time.Second,
		FailureThreshold: 2,
		SuccessThreshold: 2,
	}

	m.runOnce(context.Background())
	if r.Snapshot()[0].Healthy() {
		t.Fatal("backend became healthy before success threshold")
	}
	m.runOnce(context.Background())
	if !r.Snapshot()[0].Healthy() {
		t.Fatal("backend did not become healthy")
	}
	fail = true
	m.runOnce(context.Background())
	if !r.Snapshot()[0].Healthy() {
		t.Fatal("backend became unhealthy before failure threshold")
	}
	m.runOnce(context.Background())
	if r.Snapshot()[0].Healthy() {
		t.Fatal("backend did not become unhealthy")
	}
}
