package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rasooll/egressshuffle/internal/config"
	"github.com/rasooll/egressshuffle/internal/logging"
	"github.com/rasooll/egressshuffle/internal/server"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	showVersion := flag.Bool("version", false, "print build information and exit")
	healthcheck := flag.Bool("healthcheck", false, "check the local admin health endpoint and exit")
	flag.Parse()
	build := server.NewBuildInfo(version, commit, buildTime)
	if *showVersion {
		_ = json.NewEncoder(os.Stdout).Encode(build)
		return 0
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 2
	}
	if *healthcheck {
		return runHealthcheck(cfg.AdminAddress)
	}
	logger, err := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging error: %v\n", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := server.Run(ctx, cfg, logger, build); err != nil {
		logger.Error("EgressShuffle exited with an error", "error", err)
		return 1
	}
	return 0
}

func runHealthcheck(adminAddress string) int {
	_, port, err := net.SplitHostPort(adminAddress)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck address error: %v\n", err)
		return 2
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck returned status %d\n", response.StatusCode)
		return 1
	}
	return 0
}
