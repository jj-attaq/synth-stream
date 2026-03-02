package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jj-attaq/synth-stream/internal/auth"
	"github.com/jj-attaq/synth-stream/internal/db"
)

// querier is the subset of db.Queries used by the API server.
// Using an interface allows tests to inject a mock without a real database.
type querier interface {
	CreateUser(ctx context.Context, arg db.CreateUserParams) (db.User, error)
	GetUserByUsername(ctx context.Context, username string) (db.User, error)
}

type Server struct {
	queries   querier
	jwtSecret string
	httpServer *http.Server
}

func New(queries querier, jwtSecret, address string) *Server {
	s := &Server{
		queries:   queries,
		jwtSecret: jwtSecret,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /register", s.handleRegister)
	mux.HandleFunc("POST /login", s.handleLogin)

	s.httpServer = &http.Server{
		Addr:    address,
		Handler: mux,
	}

	return s
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't decode parameters", err)
		return
	}
	hashedPW, err := auth.HashPassword(req.Password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't hash password", err)
		return
	}
	user, err := s.queries.CreateUser(r.Context(), db.CreateUserParams{
		Username:     req.Username,
		PasswordHash: hashedPW,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create user", err)
		return
	}
	type response struct {
		ID        uuid.UUID `json:"id"`
		Username  string    `json:"username"`
		CreatedAt time.Time `json:"created_at"`
	}
	respondWithJSON(w, http.StatusCreated, response{
		ID:        uuid.UUID(user.ID.Bytes),
		Username:  user.Username,
		CreatedAt: user.CreatedAt.Time,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	type response struct {
		ID        uuid.UUID `json:"id"`
		Username  string    `json:"username"`
		CreatedAt time.Time `json:"created_at"`
		Token     string    `json:"token"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't decode parameters", err)
		return
	}
	user, err := s.queries.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized", err)
		return
	}
	if err := auth.CheckPasswordHash(req.Password, user.PasswordHash); err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized", err)
		return
	}
	accessToken, err := auth.MakeJWT(user.Username, s.jwtSecret, time.Hour)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create token", err)
		return
	}
	respondWithJSON(w, http.StatusOK, response{
		ID:        uuid.UUID(user.ID.Bytes),
		Username:  user.Username,
		CreatedAt: user.CreatedAt.Time,
		Token:     accessToken,
	})
}
