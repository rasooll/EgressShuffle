// Package proxy implements the HTTP forward proxy and CONNECT tunnels.
package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rasooll/egressshuffle/internal/backend"
	"github.com/rasooll/egressshuffle/internal/loadbalancer"
)

type Observer interface {
	Request(method, result string, duration time.Duration)
	BackendConnection(backendID, result string)
	ActiveConnections(delta int64)
}

type Handler struct {
	Registry          *backend.Registry
	LoadBalancer      loadbalancer.LoadBalancer
	Dialer            Dialer
	Authenticator     Authenticator
	ConnectTimeout    time.Duration
	RequestTimeout    time.Duration
	MaxBackendRetries int
	Logger            *slog.Logger
	Observer          Observer

	closing atomic.Bool
	tunnels tunnelRegistry
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	requestID := requestID(r.Header.Get("X-Request-ID"))
	result := "error"
	status := http.StatusBadGateway
	defer func() {
		if h.Observer != nil {
			h.Observer.Request(r.Method, result, time.Since(started))
		}
		h.Logger.LogAttrs(r.Context(), slog.LevelInfo, "proxy request completed",
			slog.String("request_id", requestID), slog.String("method", r.Method),
			slog.Int("status", status), slog.Int64("duration_ms", time.Since(started).Milliseconds()))
	}()

	if h.closing.Load() {
		status = http.StatusServiceUnavailable
		http.Error(w, "proxy is shutting down", status)
		return
	}
	if !h.Authenticator.Authorized(r) {
		status = http.StatusProxyAuthRequired
		w.Header().Set("Proxy-Authenticate", `Basic realm="EgressShuffle"`)
		http.Error(w, "proxy authentication required", status)
		return
	}
	r.Header.Del("Proxy-Authorization")
	r.Header.Del("X-Request-ID")

	if r.Method == http.MethodConnect {
		status = h.handleConnect(w, r)
	} else {
		status = h.handleHTTP(w, r)
	}
	if status < 400 {
		result = "success"
	}
}

func (h *Handler) handleHTTP(w http.ResponseWriter, r *http.Request) int {
	if r.URL.Scheme != "http" || r.URL.Host == "" {
		http.Error(w, "only absolute HTTP URLs are supported", http.StatusBadRequest)
		return http.StatusBadRequest
	}
	target, err := targetAddress(r.URL.Host, "80")
	if err != nil {
		http.Error(w, "invalid destination", http.StatusBadRequest)
		return http.StatusBadRequest
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.RequestTimeout)
	defer cancel()
	outbound := r.Clone(ctx)
	outbound.RequestURI = ""
	removeHopHeaders(outbound.Header)

	transport := &http.Transport{
		Proxy:                 nil,
		DisableCompression:    false,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     false,
		ResponseHeaderTimeout: h.RequestTimeout,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return h.dial(ctx, target)
		},
	}
	defer transport.CloseIdleConnections()
	response, err := transport.RoundTrip(outbound)
	if err != nil {
		h.Logger.WarnContext(r.Context(), "HTTP forwarding failed", "error", err)
		http.Error(w, "upstream connection failed", http.StatusBadGateway)
		return http.StatusBadGateway
	}
	defer response.Body.Close()
	removeHopHeaders(response.Header)
	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	if _, err := io.Copy(w, response.Body); err != nil {
		h.Logger.DebugContext(r.Context(), "response copy ended", "error", err)
	}
	return response.StatusCode
}

func (h *Handler) handleConnect(w http.ResponseWriter, r *http.Request) int {
	target, err := targetAddress(r.Host, "")
	if err != nil {
		http.Error(w, "CONNECT destination must be host:port", http.StatusBadRequest)
		return http.StatusBadRequest
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.ConnectTimeout)
	upstream, err := h.dial(ctx, target)
	cancel()
	if err != nil {
		h.Logger.WarnContext(r.Context(), "CONNECT upstream failed", "error", err)
		http.Error(w, "upstream connection failed", http.StatusBadGateway)
		return http.StatusBadGateway
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(w, "connection hijacking unavailable", http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		return http.StatusInternalServerError
	}
	if !h.tunnels.add(client, upstream) {
		_ = client.Close()
		_ = upstream.Close()
		return http.StatusServiceUnavailable
	}
	defer h.tunnels.remove(client, upstream)

	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		_ = client.Close()
		_ = upstream.Close()
		return http.StatusBadGateway
	}
	if err := buffered.Flush(); err != nil {
		_ = client.Close()
		_ = upstream.Close()
		return http.StatusBadGateway
	}
	if buffered.Reader.Buffered() > 0 {
		if _, err := io.CopyN(upstream, buffered, int64(buffered.Reader.Buffered())); err != nil {
			_ = client.Close()
			_ = upstream.Close()
			return http.StatusBadGateway
		}
	}
	tunnel(client, upstream)
	return http.StatusOK
}

func (h *Handler) dial(ctx context.Context, target string) (net.Conn, error) {
	items := h.Registry.Snapshot()
	attempted := make(map[string]struct{}, h.MaxBackendRetries+1)
	var lastErr error
	for attempt := 0; attempt <= h.MaxBackendRetries; attempt++ {
		candidates := unattempted(items, attempted)
		selected, err := h.LoadBalancer.Select(candidates)
		if err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, err
		}
		attempted[selected.ID()] = struct{}{}

		connectCtx, cancel := context.WithTimeout(ctx, h.ConnectTimeout)
		conn, err := h.Dialer.DialContext(connectCtx, selected.Address(), target)
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("connect through backend %s: %w", selected.ID(), err)
			if h.Observer != nil {
				h.Observer.BackendConnection(selected.ID(), "failure")
			}
			continue
		}
		if h.Observer != nil {
			h.Observer.BackendConnection(selected.ID(), "success")
			h.Observer.ActiveConnections(1)
		}
		release := selected.BeginConnection()
		return &trackedConn{Conn: conn, close: func() {
			release()
			if h.Observer != nil {
				h.Observer.ActiveConnections(-1)
			}
		}}, nil
	}
	return nil, lastErr
}

// Shutdown prevents new tunnels, waits for active tunnels, then force-closes
// them if the shutdown deadline expires.
func (h *Handler) Shutdown(ctx context.Context) error {
	h.closing.Store(true)
	h.tunnels.closeRegistration()
	done := make(chan struct{})
	go func() {
		h.tunnels.wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		h.tunnels.closeAll()
		<-done
		return ctx.Err()
	}
}

type trackedConn struct {
	net.Conn
	once  sync.Once
	close func()
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.close)
	return err
}

func targetAddress(authority, defaultPort string) (string, error) {
	if authority == "" {
		return "", errors.New("empty destination")
	}
	host, port, err := net.SplitHostPort(authority)
	if err != nil && defaultPort != "" {
		host, port, err = net.SplitHostPort(net.JoinHostPort(authority, defaultPort))
	}
	if err != nil || host == "" || port == "" {
		return "", errors.New("destination must include a valid host and port")
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return "", errors.New("invalid destination port")
	}
	return net.JoinHostPort(host, port), nil
}

func unattempted(items []*backend.Backend, attempted map[string]struct{}) []*backend.Backend {
	if len(attempted) == 0 {
		return items
	}
	result := make([]*backend.Backend, 0, len(items)-len(attempted))
	for _, item := range items {
		if _, exists := attempted[item.ID()]; !exists {
			result = append(result, item)
		}
	}
	return result
}

func requestID(provided string) string {
	if len(provided) > 0 && len(provided) <= 64 && strings.IndexFunc(provided, func(r rune) bool {
		return r < 0x21 || r > 0x7e
	}) == -1 {
		return provided
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("local-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(random)
}

func copyHeaders(destination, source http.Header) {
	for key, values := range source {
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

var hopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

func removeHopHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for token := range strings.SplitSeq(value, ",") {
			header.Del(strings.TrimSpace(token))
		}
	}
	for _, key := range hopHeaders {
		header.Del(key)
	}
}

func tunnel(client, upstream net.Conn) {
	var wg sync.WaitGroup
	copyOneWay := func(destination, source net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(destination, source)
		if closer, ok := destination.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		} else {
			_ = destination.Close()
		}
	}
	wg.Add(2)
	go copyOneWay(upstream, client)
	go copyOneWay(client, upstream)
	wg.Wait()
	_ = client.Close()
	_ = upstream.Close()
}

type tunnelRegistry struct {
	mu      sync.Mutex
	closing bool
	conns   map[net.Conn]net.Conn
	wg      sync.WaitGroup
}

func (r *tunnelRegistry) add(client, upstream net.Conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return false
	}
	if r.conns == nil {
		r.conns = make(map[net.Conn]net.Conn)
	}
	r.conns[client] = upstream
	r.wg.Add(1)
	return true
}

func (r *tunnelRegistry) remove(client, upstream net.Conn) {
	r.mu.Lock()
	delete(r.conns, client)
	r.mu.Unlock()
	r.wg.Done()
}

func (r *tunnelRegistry) closeRegistration() {
	r.mu.Lock()
	r.closing = true
	r.mu.Unlock()
}

func (r *tunnelRegistry) closeAll() {
	r.mu.Lock()
	for client, upstream := range r.conns {
		_ = client.Close()
		_ = upstream.Close()
	}
	r.mu.Unlock()
}

func (r *tunnelRegistry) wait() { r.wg.Wait() }
