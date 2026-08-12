package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
