package proxy

import (
	"net/http"
	"testing"
)

func TestRemoveHopHeaders(t *testing.T) {
	header := http.Header{
		"Connection":          {"keep-alive, X-Internal"},
		"Keep-Alive":          {"timeout=5"},
		"X-Internal":          {"secret"},
		"Proxy-Authorization": {"Basic secret"},
		"X-End-To-End":        {"preserve"},
	}
	removeHopHeaders(header)
	for _, key := range []string{"Connection", "Keep-Alive", "X-Internal", "Proxy-Authorization"} {
		if header.Get(key) != "" {
			t.Errorf("header %s was not removed", key)
		}
	}
	if header.Get("X-End-To-End") != "preserve" {
		t.Fatal("end-to-end header was removed")
	}
}

func TestTargetAddress(t *testing.T) {
	tests := []struct {
		authority string
		fallback  string
		want      string
		wantErr   bool
	}{
		{"example.com", "80", "example.com:80", false},
		{"example.com:443", "", "example.com:443", false},
		{"[2001:db8::1]:443", "", "[2001:db8::1]:443", false},
		{"missing-port", "", "", true},
		{"example.com:70000", "", "", true},
	}
	for _, tt := range tests {
		got, err := targetAddress(tt.authority, tt.fallback)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("targetAddress(%q) = %q, %v; want %q, error %v", tt.authority, got, err, tt.want, tt.wantErr)
		}
	}
}
