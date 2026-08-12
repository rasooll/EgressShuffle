// Package server wires EgressShuffle's runtime servers and workers.
package server

import (
	"encoding/json"
	"net/http"
	"runtime"

	"github.com/rasooll/egressshuffle/internal/backend"
)

type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

func NewBuildInfo(version, commit, buildTime string) BuildInfo {
	return BuildInfo{Version: version, Commit: commit, BuildTime: buildTime, GoVersion: runtime.Version()}
}

func AdminHandler(registry *backend.Registry, metrics http.Handler, build BuildInfo) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		_, healthy := registry.Counts()
		if !registry.DiscoveryComplete() || healthy == 0 {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	mux.Handle("GET /metrics", metrics)
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(build)
	})
	return mux
}
