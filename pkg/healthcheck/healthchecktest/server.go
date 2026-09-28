package healthchecktest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

const CheckPath = "/5bf66975-d4c7-4bf5-bcc8-b8d8a82ea278"

type Ping struct {
	Body   string
	Method string
	Signal string
}

type Server struct {
	*httptest.Server
	mu     sync.Mutex
	pings  []Ping
	status int
}

func New() *Server {
	s := &Server{status: http.StatusOK}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *Server) CheckURL() string { return s.URL + CheckPath }

func (s *Server) Pings() []Ping {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Ping(nil), s.pings...)
}

func (s *Server) Signals() []string {
	var out []string
	for _, p := range s.Pings() {
		out = append(out, p.Signal)
	}
	return out
}

func (s *Server) SetStatus(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, CheckPath)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pings = append(s.pings, Ping{Body: string(body), Method: r.Method, Signal: strings.TrimPrefix(rest, "/")})
	w.WriteHeader(s.status)
	_, _ = w.Write([]byte("OK"))
}
