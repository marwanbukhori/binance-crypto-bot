package dashboard

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"tradebot/internal/control"
	"tradebot/internal/store"
)

type Server struct {
	store *store.Store
	ctrl  *control.Controller
	token string
}

func New(st *store.Store, ctrl *control.Controller, token string) *Server {
	return &Server{store: st, ctrl: ctrl, token: token}
}

// tokenEqual compares two strings in constant time to prevent timing attacks.
func tokenEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// extractToken returns the presented token from, in order:
//  1. Authorization: Bearer <token> header (strict — only with the exact "Bearer " prefix)
//  2. dash_token cookie
//  3. ?token= query param (first-visit / API clients)
func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("dash_token"); err == nil {
		return c.Value
	}
	return r.URL.Query().Get("token")
}

func (s *Server) auth(r *http.Request) bool {
	if s.token == "" {
		return false
	}
	return tokenEqual(extractToken(r), s.token)
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// requirePost rejects any request that is not an HTTP POST with 405.
func requirePost(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": s.ctrl.Status(), "paused": s.ctrl.Paused()})
	}))
	mux.HandleFunc("/api/trades", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		ts, _ := s.store.ListTrades(200)
		writeJSON(w, ts)
	}))
	mux.HandleFunc("/api/signals", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		sg, _ := s.store.ListSignals(200)
		writeJSON(w, sg)
	}))
	mux.HandleFunc("/api/equity", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		eq, _ := s.store.EquitySeries(500)
		writeJSON(w, eq)
	}))
	mux.HandleFunc("/api/stats", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		st, _ := s.store.PerfStats()
		writeJSON(w, st)
	}))
	mux.HandleFunc("/api/kill", s.authMiddleware(requirePost(func(w http.ResponseWriter, r *http.Request) {
		s.ctrl.RequestKill()
		writeJSON(w, map[string]string{"ok": "kill requested"})
	})))
	mux.HandleFunc("/api/pause", s.authMiddleware(requirePost(func(w http.ResponseWriter, r *http.Request) {
		s.ctrl.Pause()
		writeJSON(w, map[string]string{"ok": "paused"})
	})))
	mux.HandleFunc("/api/resume", s.authMiddleware(requirePost(func(w http.ResponseWriter, r *http.Request) {
		s.ctrl.Resume()
		writeJSON(w, map[string]string{"ok": "resumed"})
	})))
	mux.HandleFunc("/", s.authMiddleware(s.handleIndex))
	return mux
}
