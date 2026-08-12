package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"

	"github.com/rasooll/egressshuffle/internal/backend"
	"github.com/rasooll/egressshuffle/internal/config"
	"github.com/rasooll/egressshuffle/internal/discovery"
	"github.com/rasooll/egressshuffle/internal/health"
	"github.com/rasooll/egressshuffle/internal/loadbalancer"
	"github.com/rasooll/egressshuffle/internal/metrics"
	proxyhandler "github.com/rasooll/egressshuffle/internal/proxy"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger, build BuildInfo) error {
	registry := backend.NewRegistry()
	telemetry := metrics.New(registry, cfg.LoadBalancer)
	lb, err := loadbalancer.New(cfg.LoadBalancer)
	if err != nil {
		return fmt.Errorf("create load balancer: %w", err)
	}
	socksDialer := proxyhandler.SOCKS5Dialer{NetDialer: net.Dialer{Timeout: cfg.ConnectTimeout}}
	proxy := &proxyhandler.Handler{
		Registry:          registry,
		LoadBalancer:      lb,
		Dialer:            socksDialer,
		Authenticator:     proxyhandler.NewAuthenticator(cfg.ProxyAuthEnabled, cfg.ProxyAuthUsername, cfg.ProxyAuthPassword),
		ConnectTimeout:    cfg.ConnectTimeout,
		RequestTimeout:    cfg.RequestTimeout,
		MaxBackendRetries: cfg.MaxBackendRetries,
		Logger:            logger,
		Observer:          telemetry,
	}

	checker := health.Checker(health.SOCKSChecker{Dialer: net.Dialer{Timeout: cfg.BackendHealthTimeout}})
	if cfg.TorE2EHealthcheckEnabled {
		healthURL, parseErr := url.Parse(cfg.TorE2EHealthcheckURL)
		if parseErr != nil {
			return fmt.Errorf("parse end-to-end health URL: %w", parseErr)
		}
		checker = health.E2EChecker{Base: checker, Dialer: socksDialer, URL: healthURL}
	}
	discoveryManager := &discovery.Manager{
		Discoverer: discovery.DNS{Resolver: net.DefaultResolver, Service: cfg.TorServiceName, Port: cfg.TorSOCKSPort},
		Registry:   registry, Interval: cfg.DiscoveryInterval, Observer: telemetry,
	}
	healthManager := &health.Manager{
		Registry: registry, Checker: checker, Interval: cfg.BackendHealthInterval,
		Timeout: cfg.BackendHealthTimeout, FailureThreshold: cfg.BackendFailureThreshold,
		SuccessThreshold: cfg.BackendSuccessThreshold, Observer: telemetry,
	}

	proxyServer := &http.Server{
		Addr: cfg.ProxyAddress, Handler: proxy, ReadHeaderTimeout: cfg.HeaderTimeout,
		IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: 1 << 20,
	}
	adminServer := &http.Server{
		Addr: cfg.AdminAddress, Handler: AdminHandler(registry, telemetry, build),
		ReadHeaderTimeout: cfg.HeaderTimeout, ReadTimeout: cfg.HeaderTimeout,
		WriteTimeout: cfg.HeaderTimeout, IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: 1 << 20,
	}
	proxyListener, err := net.Listen("tcp", cfg.ProxyAddress)
	if err != nil {
		return fmt.Errorf("listen on proxy address %s: %w", cfg.ProxyAddress, err)
	}
	adminListener, err := net.Listen("tcp", cfg.AdminAddress)
	if err != nil {
		_ = proxyListener.Close()
		return fmt.Errorf("listen on admin address %s: %w", cfg.AdminAddress, err)
	}

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	go discoveryManager.Run(workerCtx)
	go healthManager.Run(workerCtx)

	serverErrors := make(chan error, 2)
	go serve(proxyServer, proxyListener, "proxy", serverErrors)
	go serve(adminServer, adminListener, "admin", serverErrors)
	logger.Info("EgressShuffle started", "proxy_address", cfg.ProxyAddress, "admin_address", cfg.AdminAddress, "load_balancer", cfg.LoadBalancer)

	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case runErr = <-serverErrors:
		logger.Error("server stopped unexpectedly", "error", runErr)
	}
	cancelWorkers()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()
	var wg sync.WaitGroup
	for _, shutdown := range []func(context.Context) error{proxyServer.Shutdown, adminServer.Shutdown, proxy.Shutdown} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				logger.Warn("shutdown component failed", "error", err)
			}
		}()
	}
	wg.Wait()
	logger.Info("EgressShuffle stopped")
	return runErr
}

func serve(server *http.Server, listener net.Listener, name string, errCh chan<- error) {
	err := server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- fmt.Errorf("%s server: %w", name, err)
	}
}
