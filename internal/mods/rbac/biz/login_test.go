package biz

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/pkg/crypto/hash"
	"github.com/GoRustC/new-goadmin/pkg/jwtx"
)

func TestVerifyRootPassword(t *testing.T) {
	const given = "6351623c8cef86fefabfa7da046fc619"
	bcrypted, err := hash.GeneratePassword(given)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		configured string
		given      string
		want       bool
	}{
		{"plain match", given, given, true},
		{"plain mismatch", given, "wrong", false},
		{"bcrypt match", bcrypted, given, true},
		{"bcrypt mismatch", bcrypted, "wrong", false},
		{"empty configured disables root", "", "", false},
		{"empty given", given, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := verifyRootPassword(tt.configured, tt.given); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionExpired(t *testing.T) {
	cfg := new(config.Config)
	cfg.Middleware.Auth.MaxSessionHours = 168
	l := &Login{Config: cfg}

	claims := func(start time.Time) *jwtx.Claims {
		return &jwtx.Claims{AuthTime: jwt.NewNumericDate(start)}
	}
	if l.sessionExpired(claims(time.Now().Add(-24 * time.Hour))) {
		t.Error("a one-day-old session should be valid")
	}
	if !l.sessionExpired(claims(time.Now().Add(-8 * 24 * time.Hour))) {
		t.Error("an eight-day-old session should be expired")
	}
	legacy := &jwtx.Claims{}
	legacy.IssuedAt = jwt.NewNumericDate(time.Now().Add(-8 * 24 * time.Hour))
	if !l.sessionExpired(legacy) {
		t.Error("a token without auth_time should be aged by its issue time")
	}
	cfg.Middleware.Auth.MaxSessionHours = 0
	if l.sessionExpired(claims(time.Now().Add(-365 * 24 * time.Hour))) {
		t.Error("0 must disable the limit")
	}
}
