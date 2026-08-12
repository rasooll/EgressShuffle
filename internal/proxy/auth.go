package proxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

type Authenticator struct {
	enabled      bool
	usernameHash [sha256.Size]byte
	passwordHash [sha256.Size]byte
}

func NewAuthenticator(enabled bool, username, password string) Authenticator {
	return Authenticator{
		enabled:      enabled,
		usernameHash: sha256.Sum256([]byte(username)),
		passwordHash: sha256.Sum256([]byte(password)),
	}
}

func (a Authenticator) Authorized(r *http.Request) bool {
	if !a.enabled {
		return true
	}
	username, password, ok := parseProxyBasicAuth(r.Header.Get("Proxy-Authorization"))
	if !ok {
		return false
	}
	usernameHash := sha256.Sum256([]byte(username))
	passwordHash := sha256.Sum256([]byte(password))
	usernameOK := subtle.ConstantTimeCompare(usernameHash[:], a.usernameHash[:])
	passwordOK := subtle.ConstantTimeCompare(passwordHash[:], a.passwordHash[:])
	return usernameOK&passwordOK == 1
}

func parseProxyBasicAuth(value string) (username, password string, ok bool) {
	r := &http.Request{Header: http.Header{"Authorization": []string{value}}}
	return r.BasicAuth()
}
