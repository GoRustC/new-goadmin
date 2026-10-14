package biz

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/pkg/cachex"
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

// Concurrent failed logins must all count toward the lock.
func TestLoginLockCountsConcurrentFailures(t *testing.T) {
	cfg := new(config.Config)
	cfg.General.LoginLock.MaxFailures = 100
	cfg.General.LoginLock.LockMinutes = 15
	l := loginLock{cache: cachex.NewMemoryCache(cachex.MemoryConfig{CleanupInterval: time.Minute}), cfg: cfg}
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if err := l.fail(ctx, "alice"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	if n, err := l.failures(ctx, "alice"); err != nil || n != 100 {
		t.Fatalf("failures = %d, %v; want 100", n, err)
	}
	if err := l.check(ctx, "alice"); err == nil {
		t.Fatal("the name should be locked after 100 failures")
	}
}
