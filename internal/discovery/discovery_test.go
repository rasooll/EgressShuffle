package discovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/rasooll/egressshuffle/internal/backend"
)

type resolverFunc func(context.Context, string) ([]string, error)

func (f resolverFunc) LookupHost(ctx context.Context, host string) ([]string, error) {
	return f(ctx, host)
}

func TestDNSDiscoverNormalizesAndDeduplicates(t *testing.T) {
	d := DNS{
		Resolver: resolverFunc(func(context.Context, string) ([]string, error) {
			return []string{"10.0.0.2", "10.0.0.1", "10.0.0.2", "invalid"}, nil
		}),
		Service: "tor",
		Port:    9050,
	}
	got, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	want := []string{"10.0.0.1:9050", "10.0.0.2:9050"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Discover() = %v, want %v", got, want)
	}
}

type discovererFunc func(context.Context) ([]string, error)

func (f discovererFunc) Discover(ctx context.Context) ([]string, error) { return f(ctx) }

func TestManagerRetainsBackendsOnDiscoveryFailure(t *testing.T) {
	r := backend.NewRegistry()
	r.Reconcile([]string{"10.0.0.1:9050"})
	m := Manager{
		Discoverer: discovererFunc(func(context.Context) ([]string, error) {
			return nil, errors.New("temporary DNS failure")
		}),
		Registry: r,
		Interval: time.Hour,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.Run(ctx)
	if got := len(r.Snapshot()); got != 1 {
		t.Fatalf("backend count = %d, want retained count 1", got)
	}
}
