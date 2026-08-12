package proxy

import (
	"encoding/base64"
	"net/http"
	"testing"
)

func TestAuthenticator(t *testing.T) {
	auth := NewAuthenticator(true, "user", "correct horse")
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"valid", "Basic " + base64.StdEncoding.EncodeToString([]byte("user:correct horse")), true},
		{"wrong password", "Basic " + base64.StdEncoding.EncodeToString([]byte("user:wrong")), false},
		{"wrong scheme", "Bearer token", false},
		{"malformed", "Basic !!!", false},
		{"missing", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{Header: make(http.Header)}
			r.Header.Set("Proxy-Authorization", tt.value)
			if got := auth.Authorized(r); got != tt.want {
				t.Fatalf("Authorized() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDisabledAuthenticator(t *testing.T) {
	if !NewAuthenticator(false, "", "").Authorized(&http.Request{}) {
		t.Fatal("disabled authenticator rejected request")
	}
}
