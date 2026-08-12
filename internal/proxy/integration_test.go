package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rasooll/egressshuffle/internal/backend"
	"github.com/rasooll/egressshuffle/internal/loadbalancer"
)

func TestHTTPProxyThroughSOCKS5(t *testing.T) {
	var receivedProxyAuth, receivedInternal string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedProxyAuth = r.Header.Get("Proxy-Authorization")
		receivedInternal = r.Header.Get("X-Remove-Me")
		w.Header().Set("X-Upstream", "yes")
		_, _ = w.Write([]byte("through Tor"))
	}))
	defer upstream.Close()
	upstreamAddress := upstream.Listener.Addr().String()
	_, port, _ := net.SplitHostPort(upstreamAddress)
	target := net.JoinHostPort("upstream.test", port)
	socks := startFakeSOCKS(t, map[string]string{target: upstreamAddress})

	handler := testHandler(t, []string{socks.address()}, NewAuthenticator(true, "proxy-user", "proxy-password"), 1)
	proxyServer := httptest.NewServer(handler)
	defer proxyServer.Close()
	proxyURL, _ := url.Parse(proxyServer.URL)
	proxyURL.User = url.UserPassword("proxy-user", "proxy-password")
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}

	request, _ := http.NewRequest(http.MethodGet, "http://"+target+"/resource?private=query", nil)
	request.Header.Set("Connection", "X-Remove-Me")
	request.Header.Set("X-Remove-Me", "must-not-arrive")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("HTTP proxy request failed: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "through Tor" {
		t.Fatalf("response = %d %q", response.StatusCode, body)
	}
	if receivedProxyAuth != "" || receivedInternal != "" {
		t.Fatalf("hop headers reached upstream: proxy auth %q, internal %q", receivedProxyAuth, receivedInternal)
	}
	if got := <-socks.destinations; got != target {
		t.Fatalf("SOCKS5 destination = %q, want hostname %q", got, target)
	}
}

func TestHTTPSConnectThroughSOCKS5(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secure tunnel"))
	}))
	defer upstream.Close()
	upstreamAddress := upstream.Listener.Addr().String()
	_, port, _ := net.SplitHostPort(upstreamAddress)
	target := net.JoinHostPort("tls.test", port)
	socks := startFakeSOCKS(t, map[string]string{target: upstreamAddress})

	handler := testHandler(t, []string{socks.address()}, NewAuthenticator(false, "", ""), 0)
	proxyServer := httptest.NewServer(handler)
	defer proxyServer.Close()
	proxyURL, _ := url.Parse(proxyServer.URL)
	transport := &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Test server certificate is intentionally local.
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Get("https://" + target + "/")
	if err != nil {
		t.Fatalf("HTTPS proxy request failed: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "secure tunnel" {
		t.Fatalf("response = %d %q", response.StatusCode, body)
	}
	if got := <-socks.destinations; got != target {
		t.Fatalf("SOCKS5 destination = %q, want %q", got, target)
	}
}

func TestProxyAuthenticationRequired(t *testing.T) {
	handler := testHandler(t, nil, NewAuthenticator(true, "user", "password"), 0)
	server := httptest.NewServer(handler)
	defer server.Close()
	proxyURL, _ := url.Parse(server.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	response, err := client.Get("http://example.invalid/")
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("status = %d, want 407", response.StatusCode)
	}
	if response.Header.Get("Proxy-Authenticate") == "" {
		t.Fatal("407 response omitted Proxy-Authenticate")
	}
}

func TestBackendFailoverBeforeRequestTransmission(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	upstreamAddress := upstream.Listener.Addr().String()
	_, port, _ := net.SplitHostPort(upstreamAddress)
	target := net.JoinHostPort("failover.test", port)
	socks := startFakeSOCKS(t, map[string]string{target: upstreamAddress})

	handler := testHandler(t, []string{"127.0.0.1:1", socks.address()}, NewAuthenticator(false, "", ""), 1)
	server := httptest.NewServer(handler)
	defer server.Close()
	proxyURL, _ := url.Parse(server.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	response, err := client.Get("http://" + target + "/")
	if err != nil {
		t.Fatalf("failover request failed: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}
}

func TestRequestIsNotReplayedAfterDial(t *testing.T) {
	dialer := &closeAfterRequestDialer{}
	handler := testHandlerWithDialer(t, []string{"127.0.0.1:1001", "127.0.0.1:1002"}, dialer, 1)
	server := httptest.NewServer(handler)
	defer server.Close()
	proxyURL, _ := url.Parse(server.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	response, err := client.Get("http://not-replayed.test/")
	if err != nil {
		t.Fatalf("proxy returned a transport error: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", response.StatusCode)
	}
	if got := dialer.calls.Load(); got != 1 {
		t.Fatalf("dial calls = %d, request was replayed after transmission", got)
	}
}

func TestBackendRetryLimit(t *testing.T) {
	dialer := &alwaysFailDialer{}
	handler := testHandlerWithDialer(t, []string{
		"127.0.0.1:1001", "127.0.0.1:1002", "127.0.0.1:1003",
	}, dialer, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := handler.dial(ctx, "example.test:80"); err == nil {
		t.Fatal("dial() expected retry exhaustion error")
	}
	if got := dialer.calls.Load(); got != 2 {
		t.Fatalf("dial attempts = %d, want initial attempt plus one retry", got)
	}
}

func TestShutdownClosesLongLivedConnectTunnel(t *testing.T) {
	targetListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer targetListener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := targetListener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	socks := startFakeSOCKS(t, nil)
	handler := testHandler(t, []string{socks.address()}, NewAuthenticator(false, "", ""), 0)
	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	target := targetListener.Addr().String()
	_, _ = fmt.Fprintf(client, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	response, err := http.ReadResponse(bufio.NewReader(client), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d", response.StatusCode)
	}
	upstream := <-accepted
	defer upstream.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := handler.Shutdown(ctx); err != context.DeadlineExceeded {
		t.Fatalf("Shutdown() error = %v, want deadline exceeded after forced close", err)
	}
	buffer := make([]byte, 1)
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(buffer); err == nil {
		t.Fatal("client connection remained open after forced shutdown")
	}
	_ = client.Close()
}

func testHandler(t *testing.T, addresses []string, auth Authenticator, retries int) *Handler {
	t.Helper()
	return testHandlerWithDialerAndAuth(t, addresses, SOCKS5Dialer{NetDialer: net.Dialer{Timeout: time.Second}}, auth, retries)
}

func testHandlerWithDialer(t *testing.T, addresses []string, dialer Dialer, retries int) *Handler {
	t.Helper()
	return testHandlerWithDialerAndAuth(t, addresses, dialer, NewAuthenticator(false, "", ""), retries)
}

func testHandlerWithDialerAndAuth(t *testing.T, addresses []string, dialer Dialer, auth Authenticator, retries int) *Handler {
	t.Helper()
	registry := backend.NewRegistry()
	registry.Reconcile(addresses)
	for _, item := range registry.Snapshot() {
		item.ObserveHealth(true, 1, 1, time.Now())
	}
	return &Handler{
		Registry: registry, LoadBalancer: &loadbalancer.RoundRobin{}, Dialer: dialer,
		Authenticator: auth, ConnectTimeout: time.Second, RequestTimeout: 5 * time.Second,
		MaxBackendRetries: retries, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

type closeAfterRequestDialer struct {
	calls atomic.Int64
}

type alwaysFailDialer struct {
	calls atomic.Int64
}

func (d *alwaysFailDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	d.calls.Add(1)
	return nil, fmt.Errorf("backend unavailable")
}

func (d *closeAfterRequestDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	d.calls.Add(1)
	client, server := net.Pipe()
	go func() {
		buffer := make([]byte, 1024)
		_, _ = server.Read(buffer)
		_ = server.Close()
	}()
	return client, nil
}

type fakeSOCKS struct {
	listener     net.Listener
	mappings     map[string]string
	destinations chan string
	wg           sync.WaitGroup
}

func startFakeSOCKS(t *testing.T, mappings map[string]string) *fakeSOCKS {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start fake SOCKS5: %v", err)
	}
	server := &fakeSOCKS{listener: listener, mappings: mappings, destinations: make(chan string, 16)}
	server.wg.Add(1)
	go server.serve()
	t.Cleanup(func() {
		_ = listener.Close()
		server.wg.Wait()
	})
	return server
}

func (s *fakeSOCKS) address() string { return s.listener.Addr().String() }

func (s *fakeSOCKS) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(conn)
		}()
	}
}

func (s *fakeSOCKS) handle(client net.Conn) {
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	header := make([]byte, 2)
	if _, err := io.ReadFull(client, header); err != nil || header[0] != 0x05 {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(client, methods); err != nil {
		return
	}
	if _, err := client.Write([]byte{0x05, 0x00}); err != nil {
		return
	}
	request := make([]byte, 4)
	if _, err := io.ReadFull(client, request); err != nil || request[1] != 0x01 {
		return
	}
	host, err := readSOCKSAddress(client, request[3])
	if err != nil {
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(client, portBytes); err != nil {
		return
	}
	target := net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(portBytes)))
	s.destinations <- target
	dialAddress := target
	if mapped, ok := s.mappings[target]; ok {
		dialAddress = mapped
	}
	upstream, err := net.DialTimeout("tcp", dialAddress, time.Second)
	if err != nil {
		_, _ = client.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()
	if _, err := client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	_ = client.SetDeadline(time.Time{})
	tunnel(client, upstream)
}

func readSOCKSAddress(reader io.Reader, addressType byte) (string, error) {
	var length int
	switch addressType {
	case 0x01:
		length = net.IPv4len
	case 0x03:
		size := []byte{0}
		if _, err := io.ReadFull(reader, size); err != nil {
			return "", err
		}
		length = int(size[0])
	case 0x04:
		length = net.IPv6len
	default:
		return "", fmt.Errorf("unsupported address type %d", addressType)
	}
	value := make([]byte, length)
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", err
	}
	if addressType == 0x03 {
		return string(value), nil
	}
	return net.IP(value).String(), nil
}
