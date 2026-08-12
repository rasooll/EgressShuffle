package health

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

type ContextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

// E2EChecker verifies an optional HTTP endpoint through the selected backend.
type E2EChecker struct {
	Base   Checker
	Dialer ContextDialer
	URL    *url.URL
}

func (c E2EChecker) Check(ctx context.Context, backendAddress string) error {
	if err := c.Base.Check(ctx, backendAddress); err != nil {
		return err
	}
	port := c.URL.Port()
	if port == "" {
		if c.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	target := net.JoinHostPort(c.URL.Hostname(), port)
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return c.Dialer.DialContext(ctx, backendAddress, target)
		},
	}
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL.String(), nil)
	if err != nil {
		return fmt.Errorf("create end-to-end health request: %w", err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		return fmt.Errorf("end-to-end Tor health request: %w", err)
	}
	_ = response.Body.Close()
	return nil
}
