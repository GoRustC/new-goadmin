package jwtx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAuth(t *testing.T) {
	cache := NewMemoryCache(MemoryConfig{CleanupInterval: time.Second})

	store := NewStoreWithCache(cache)
	ctx := context.Background()
	jwtAuth := New(store, SetSigningKey("test-signing-key-0123456789abcdef", ""))

	userID := "test"
	token, err := jwtAuth.GenerateToken(ctx, userID)
	assert.Nil(t, err)
	assert.NotNil(t, token)

	id, err := jwtAuth.ParseSubject(ctx, token.GetAccessToken())
	assert.Nil(t, err)
	assert.Equal(t, userID, id)

	err = jwtAuth.DestroyToken(ctx, token.GetAccessToken())
	assert.Nil(t, err)

	id, err = jwtAuth.ParseSubject(ctx, token.GetAccessToken())
	assert.NotNil(t, err)
	assert.EqualError(t, err, ErrInvalidToken.Error())
	assert.Empty(t, id)

	err = jwtAuth.Release(ctx)
	assert.Nil(t, err)
}

func TestAuthTimeAndSessionStart(t *testing.T) {
	store := NewStoreWithCache(NewMemoryCache(MemoryConfig{CleanupInterval: time.Minute}))
	auth := New(store, SetSigningKey("test-signing-key-0123456789abcdef", ""))
	ctx := context.Background()

	fresh, err := auth.GenerateToken(ctx, "u1")
	assert.Nil(t, err)
	c, err := auth.ParseClaims(ctx, fresh.GetAccessToken())
	assert.Nil(t, err)
	assert.WithinDuration(t, time.Now(), c.SessionStart(), 2*time.Second, "auth_time defaults to now")

	start := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	refreshed, err := auth.GenerateToken(ctx, "u1", WithAuthTime(start))
	assert.Nil(t, err)
	c, err = auth.ParseClaims(ctx, refreshed.GetAccessToken())
	assert.Nil(t, err)
	assert.True(t, c.SessionStart().Equal(start), "auth_time is carried over: got %v want %v", c.SessionStart(), start)

	// Tokens issued before auth_time existed fall back to iat.
	legacy := &Claims{}
	legacy.IssuedAt = c.IssuedAt
	assert.True(t, legacy.SessionStart().Equal(c.IssuedAt.Time))
}
