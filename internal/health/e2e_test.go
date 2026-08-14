package health

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type directDialer struct{ net.Dialer }

func (d directDialer) DialContext(ctx context.Context, _, target string) (net.Conn, error) {
	return d.Dialer.DialContext(ctx, "tcp", target)
}

func TestE2ECheckerValidatesStatus(t *testing.T) {
	status := http.StatusServiceUnavailable
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	defer server.Close()
	healthURL, _ := url.Parse(server.URL)
	checker := E2EChecker{
		Base:   checkerFunc(func(context.Context, string) error { return nil }),
		Dialer: directDialer{},
		URL:    healthURL,
	}
	if err := checker.Check(context.Background(), "unused"); err == nil {
		t.Fatal("Check() accepted a server error response")
	}
	status = http.StatusNoContent
	if err := checker.Check(context.Background(), "unused"); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}
