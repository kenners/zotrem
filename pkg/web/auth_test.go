package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenners/zotrem/pkg/web"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware(t *testing.T) {
	tests := []struct {
		auth     web.Auth
		login    string
		name     string
		password string
		path     string
		remote   string
		want     int
	}{
		{name: "open", path: "/", want: http.StatusOK},
		{auth: web.Auth{Password: "secret"}, name: "password missing", path: "/", want: http.StatusUnauthorized},
		{auth: web.Auth{Password: "secret"}, name: "password wrong", password: "nope", path: "/", want: http.StatusUnauthorized},
		{auth: web.Auth{Password: "secret"}, name: "password right", password: "secret", path: "/", want: http.StatusOK},
		{auth: web.Auth{Password: "secret"}, name: "password guards api", path: "/api/status", want: http.StatusUnauthorized},
		{auth: web.Auth{TailscaleUsers: []string{"alice@example.com"}}, login: "alice@example.com", name: "tailscale allowed", path: "/", want: http.StatusOK},
		{auth: web.Auth{TailscaleUsers: []string{"alice@example.com"}}, login: "Alice@Example.com", name: "tailscale case insensitive", path: "/", want: http.StatusOK},
		{auth: web.Auth{TailscaleUsers: []string{"alice@example.com"}}, login: "alice@example.com", name: "tailscale ipv6 loopback", path: "/", remote: "[::1]:5555", want: http.StatusOK},
		{auth: web.Auth{TailscaleUsers: []string{"alice@example.com"}}, login: "bob@example.com", name: "tailscale other user", path: "/", want: http.StatusForbidden},
		{auth: web.Auth{TailscaleUsers: []string{"alice@example.com"}}, name: "tailscale header missing", path: "/", want: http.StatusForbidden},
		{auth: web.Auth{TailscaleUsers: []string{"alice@example.com"}}, name: "tailscale guards api", path: "/api/sync", want: http.StatusForbidden},
		{auth: web.Auth{TailscaleUsers: []string{"alice@example.com"}}, login: "alice@example.com", name: "tailscale not from loopback", path: "/", remote: "100.64.1.2:5555", want: http.StatusForbidden},
		{auth: web.Auth{Password: "secret", TailscaleUsers: []string{"alice@example.com"}}, login: "alice@example.com", name: "both without password", path: "/", want: http.StatusUnauthorized},
		{auth: web.Auth{Password: "secret", TailscaleUsers: []string{"alice@example.com"}}, name: "both without identity", password: "secret", path: "/", want: http.StatusForbidden},
		{auth: web.Auth{Password: "secret", TailscaleUsers: []string{"alice@example.com"}}, login: "alice@example.com", name: "both satisfied", password: "secret", path: "/", want: http.StatusOK},
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.RemoteAddr = "127.0.0.1:5555"
			if tt.remote != "" {
				req.RemoteAddr = tt.remote
			}
			if tt.login != "" {
				req.Header.Set(web.TailscaleUserHeader, tt.login)
			}
			if tt.password != "" {
				req.SetBasicAuth("zotrem", tt.password)
			}
			rec := httptest.NewRecorder()
			tt.auth.Middleware(ok).ServeHTTP(rec, req)
			a.Equal(tt.want, rec.Code)
		})
	}
}

func TestAuthValidate(t *testing.T) {
	tests := []struct {
		bind    string
		name    string
		users   []string
		wantErr bool
	}{
		{bind: "0.0.0.0", name: "no tailscale users"},
		{bind: "127.0.0.1", name: "ipv4 loopback", users: []string{"alice@example.com"}},
		{bind: "::1", name: "ipv6 loopback", users: []string{"alice@example.com"}},
		{bind: "localhost", name: "localhost", users: []string{"alice@example.com"}},
		{bind: "0.0.0.0", name: "all interfaces", users: []string{"alice@example.com"}, wantErr: true},
		{bind: "100.64.1.2", name: "tailscale address", users: []string{"alice@example.com"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := web.Auth{TailscaleUsers: tt.users}.Validate(tt.bind)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestParseTailscaleUsers(t *testing.T) {
	a := assert.New(t)
	a.Equal([]string{"alice@example.com", "bob@example.com"}, web.ParseTailscaleUsers(" Alice@Example.com, ,bob@example.com "))
	a.Empty(web.ParseTailscaleUsers(""))
}
