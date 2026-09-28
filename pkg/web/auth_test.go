package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenners/zotrem/pkg/web"
	"github.com/stretchr/testify/assert"
)

func TestBasicAuth(t *testing.T) {
	tests := []struct {
		configured string
		name       string
		password   string
		path       string
		want       int
	}{
		{name: "open", path: "/", want: http.StatusOK},
		{configured: "secret", name: "password missing", path: "/", want: http.StatusUnauthorized},
		{configured: "secret", name: "password wrong", password: "nope", path: "/", want: http.StatusUnauthorized},
		{configured: "secret", name: "password right", password: "secret", path: "/", want: http.StatusOK},
		{configured: "secret", name: "guards api", path: "/api/status", want: http.StatusUnauthorized},
		{configured: "secret", name: "api with password", password: "secret", path: "/api/status", want: http.StatusOK},
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.password != "" {
				req.SetBasicAuth("zotrem", tt.password)
			}
			rec := httptest.NewRecorder()
			web.BasicAuth(tt.configured)(ok).ServeHTTP(rec, req)
			a.Equal(tt.want, rec.Code)
			if tt.want == http.StatusUnauthorized {
				a.Equal(`Basic realm="zotrem"`, rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}
