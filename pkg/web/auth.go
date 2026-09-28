package web

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
)

const TailscaleUserHeader = "Tailscale-User-Login"

type Auth struct {
	Log            *slog.Logger
	Password       string
	TailscaleUsers []string
}

func ParseTailscaleUsers(list string) []string {
	var users []string
	for _, user := range strings.Split(list, ",") {
		if user = strings.ToLower(strings.TrimSpace(user)); user != "" {
			users = append(users, user)
		}
	}
	return users
}

func (a Auth) Validate(bind string) error {
	if len(a.TailscaleUsers) == 0 || isLoopbackHost(bind) {
		return nil
	}
	return errors.Newf("ZOTREM_TAILSCALE_USERS needs zotrem bound to localhost behind tailscale serve, not %q: anyone who can reach that address could forge the %s header", bind, TailscaleUserHeader)
}

func (a Auth) Middleware(next http.Handler) http.Handler {
	if a.Password == "" && len(a.TailscaleUsers) == 0 {
		return next
	}
	log := a.Log
	if log == nil {
		log = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(a.TailscaleUsers) > 0 {
			login := strings.ToLower(strings.TrimSpace(r.Header.Get(TailscaleUserHeader)))
			if !isLoopbackRemote(r.RemoteAddr) || !a.allowed(login) {
				log.Warn("tailscale identity rejected", "login", login, "remote", r.RemoteAddr, "path", r.URL.Path)
				http.Error(w, "forbidden: open zotrem through Tailscale as an allowed user", http.StatusForbidden)
				return
			}
		}
		if a.Password != "" {
			_, pass, ok := r.BasicAuth()
			if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(a.Password)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="zotrem"`)
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a Auth) allowed(login string) bool {
	if login == "" {
		return false
	}
	for _, user := range a.TailscaleUsers {
		if user == login {
			return true
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isLoopbackRemote(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return isLoopbackHost(host)
}
