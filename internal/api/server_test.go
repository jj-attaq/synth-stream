package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jj-attaq/synth-stream/internal/auth"
	"github.com/jj-attaq/synth-stream/internal/db"
)

// mockQuerier implements the querier interface for testing — no real DB needed.
type mockQuerier struct {
	createUserFn       func(ctx context.Context, arg db.CreateUserParams) (db.User, error)
	getUserByUsernameFn func(ctx context.Context, username string) (db.User, error)
}

func (m *mockQuerier) CreateUser(ctx context.Context, arg db.CreateUserParams) (db.User, error) {
	return m.createUserFn(ctx, arg)
}

func (m *mockQuerier) GetUserByUsername(ctx context.Context, username string) (db.User, error) {
	return m.getUserByUsernameFn(ctx, username)
}

// makeTestUser builds a db.User with a bcrypt-hashed password for use in login tests.
func makeTestUser(t *testing.T, username, password string) db.User {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	return db.User{
		ID:           pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		Username:     username,
		PasswordHash: hash,
		CreatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

func newTestServer(q querier) *Server {
	return &Server{queries: q, jwtSecret: "test-secret"}
}

func postJSON(t *testing.T, handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

// --- handleRegister ---

func TestHandleRegister_Success(t *testing.T) {
	q := &mockQuerier{
		createUserFn: func(ctx context.Context, arg db.CreateUserParams) (db.User, error) {
			return db.User{
				ID:           pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
				Username:     arg.Username,
				PasswordHash: arg.PasswordHash,
				CreatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
			}, nil
		},
	}
	s := newTestServer(q)
	w := postJSON(t, s.handleRegister, map[string]string{"username": "alice", "password": "secret123"})

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["username"] != "alice" {
		t.Errorf("expected username alice, got %v", resp["username"])
	}
	if _, ok := resp["password_hash"]; ok {
		t.Error("response must not include password_hash")
	}
}

func TestHandleRegister_BadBody(t *testing.T) {
	s := newTestServer(&mockQuerier{})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	s.handleRegister(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleRegister_DBError(t *testing.T) {
	q := &mockQuerier{
		createUserFn: func(ctx context.Context, arg db.CreateUserParams) (db.User, error) {
			return db.User{}, errors.New("username already taken")
		},
	}
	s := newTestServer(q)
	w := postJSON(t, s.handleRegister, map[string]string{"username": "alice", "password": "secret123"})

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// --- handleLogin ---

func TestHandleLogin_Success(t *testing.T) {
	user := makeTestUser(t, "alice", "secret123")
	q := &mockQuerier{
		getUserByUsernameFn: func(ctx context.Context, username string) (db.User, error) {
			return user, nil
		},
	}
	s := newTestServer(q)
	w := postJSON(t, s.handleLogin, map[string]string{"username": "alice", "password": "secret123"})

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["token"] == "" || resp["token"] == nil {
		t.Error("expected a token in response")
	}
}

func TestHandleLogin_WrongPassword(t *testing.T) {
	user := makeTestUser(t, "alice", "secret123")
	q := &mockQuerier{
		getUserByUsernameFn: func(ctx context.Context, username string) (db.User, error) {
			return user, nil
		},
	}
	s := newTestServer(q)
	w := postJSON(t, s.handleLogin, map[string]string{"username": "alice", "password": "wrongpassword"})

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleLogin_UserNotFound(t *testing.T) {
	q := &mockQuerier{
		getUserByUsernameFn: func(ctx context.Context, username string) (db.User, error) {
			return db.User{}, errors.New("user not found")
		},
	}
	s := newTestServer(q)
	w := postJSON(t, s.handleLogin, map[string]string{"username": "ghost", "password": "secret123"})

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleLogin_BadBody(t *testing.T) {
	s := newTestServer(&mockQuerier{})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	s.handleLogin(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
