package dashboard

import (
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

func (s *Server) auth(r *http.Request) bool {
	if s.token == "" {
		return false
	}
	if h := r.Header.Get("Authorization"); strings.TrimPrefix(h, "Bearer ") == s.token {
		return true
	}
	return r.URL.Query().Get("token") == s.token
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
	mux.HandleFunc("/api/kill", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		s.ctrl.RequestKill()
		writeJSON(w, map[string]string{"ok": "kill requested"})
	}))
	mux.HandleFunc("/api/pause", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		s.ctrl.Pause()
		writeJSON(w, map[string]string{"ok": "paused"})
	}))
	mux.HandleFunc("/api/resume", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		s.ctrl.Resume()
		writeJSON(w, map[string]string{"ok": "resumed"})
	}))
	mux.HandleFunc("/", s.authMiddleware(s.handleIndex)) // Task 4
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }
