package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	as *AccountHandler
}

func NewServer(as *AccountHandler) *Server {
	return &Server{as: as}
}

func (s *Server) Routes(pool *pgxpool.Pool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("GET /accounts", s.as.GetAccounts)
	mux.HandleFunc("GET /accounts/{id}", s.as.GetAccount)
	mux.HandleFunc("GET /accounts/{id}/postings", s.as.GetAccountPostings)
	mux.HandleFunc("POST /accounts/{id}/deposits", s.as.Deposit)
	return mux
}
