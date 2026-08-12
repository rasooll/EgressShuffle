package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rasooll/egressshuffle/internal/backend"
)

func TestReadiness(t *testing.T) {
	registry := backend.NewRegistry()
	handler := AdminHandler(registry, http.NotFoundHandler(), NewBuildInfo("test", "commit", "now"))

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("initial readiness status = %d", response.Code)
	}

	registry.Reconcile([]string{"127.0.0.1:9050"})
	registry.Snapshot()[0].ObserveHealth(true, 1, 1, time.Now())
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("ready status = %d", response.Code)
	}
}
