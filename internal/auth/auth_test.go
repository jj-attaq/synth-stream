package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCheckPasswordHash(t *testing.T) {
	hash1, _ := HashPassword("correctPassword123!")
	hash2, _ := HashPassword("anotherPassword456!")

	tests := []struct {
		name     string
		password string
		hash     string
		wantErr  bool
	}{
		{"correct password", "correctPassword123!", hash1, false},
		{"wrong password", "wrongPassword", hash1, true},
		{"password vs different hash", "correctPassword123!", hash2, true},
		{"empty password", "", hash1, true},
		{"invalid hash", "correctPassword123!", "notahash", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckPasswordHash(tt.password, tt.hash)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckPasswordHash() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetBearerToken(t *testing.T) {
	tests := []struct {
		name      string
		header    http.Header
		wantToken string
		wantErr   bool
	}{
		{
			name:      "valid bearer token",
			header:    http.Header{"Authorization": []string{"Bearer mytoken123"}},
			wantToken: "mytoken123",
			wantErr:   false,
		},
		{
			name:      "missing authorization header",
			header:    http.Header{},
			wantToken: "",
			wantErr:   true,
		},
		{
			name:      "wrong scheme",
			header:    http.Header{"Authorization": []string{"Basic mytoken123"}},
			wantToken: "",
			wantErr:   true,
		},
		{
			name:      "empty bearer token",
			header:    http.Header{"Authorization": []string{"Bearer "}},
			wantToken: "",
			wantErr:   true,
		},
		{
			name:      "whitespace trimmed",
			header:    http.Header{"Authorization": []string{"Bearer   mytoken123   "}},
			wantToken: "mytoken123",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetBearerToken(tt.header)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetBearerToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantToken {
				t.Errorf("GetBearerToken() = %v, want %v", got, tt.wantToken)
			}
		})
	}
}

func TestMakeJWT(t *testing.T) {
	token, err := MakeJWT(uuid.New(), "alice", "secret", time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT() unexpected error: %v", err)
	}
	if token == "" {
		t.Error("MakeJWT() returned empty token")
	}
}

func TestValidateJWT(t *testing.T) {
	id := uuid.New()
	validToken, _ := MakeJWT(id, "alice", "secret", time.Hour)
	expiredToken, _ := MakeJWT(id, "alice", "secret", -1*time.Second)

	tests := []struct {
		name         string
		tokenString  string
		tokenSecret  string
		wantUsername string
		wantErr      bool
	}{
		{
			name:         "valid token",
			tokenString:  validToken,
			tokenSecret:  "secret",
			wantUsername: "alice",
			wantErr:      false,
		},
		{
			name:         "wrong secret",
			tokenString:  validToken,
			tokenSecret:  "wrong-secret",
			wantUsername: "",
			wantErr:      true,
		},
		{
			name:         "expired token",
			tokenString:  expiredToken,
			tokenSecret:  "secret",
			wantUsername: "",
			wantErr:      true,
		},
		{
			name:         "malformed token",
			tokenString:  "not.a.token",
			tokenSecret:  "secret",
			wantUsername: "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateJWT(tt.tokenString, tt.tokenSecret)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateJWT() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantUsername {
				t.Errorf("ValidateJWT() = %v, want %v", got, tt.wantUsername)
			}
		})
	}
}
