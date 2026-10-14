// Package jwtx issues and validates HMAC-signed JWT access tokens and keeps
// revoked tokens in a cache.
package jwtx

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Authenticator issues and validates access tokens.
type Authenticator interface {
	// Generate a JWT (JSON Web Token) with the provided subject.
	GenerateToken(ctx context.Context, subject string, opts ...TokenOption) (TokenInfo, error)
	// Invalidate a token by removing it from the token store.
	DestroyToken(ctx context.Context, accessToken string) error
	// Parse the subject (or user identifier) from a given access token.
	ParseSubject(ctx context.Context, accessToken string) (string, error)
	// ParseClaims validates a token, including the revocation store, and
	// returns its claims.
	ParseClaims(ctx context.Context, accessToken string) (*Claims, error)
	// Release any resources held by the JWTAuth instance.
	Release(ctx context.Context) error
}

var (
	// ErrInvalidToken is returned for a token that is malformed, expired,
	// signed with an unknown key or revoked.
	ErrInvalidToken = errors.New("jwtx: invalid token")
	// ErrMissingSigningKey is returned when no signing key is configured.
	ErrMissingSigningKey = errors.New("jwtx: signing key is required")
)

func init() {
	// Issue times with millisecond precision, so a token issued right after
	// a revocation (for example a login just after a password change) can be
	// told apart from one issued just before it in the same second. Tokens
	// with whole-second times still parse.
	jwt.TimePrecision = time.Millisecond
}

type options struct {
	signingMethod jwt.SigningMethod
	signingKey    []byte
	signingKey2   []byte
	keyFuncs      []func(*jwt.Token) (any, error)
	expired       int
	tokenType     string
}

// Option configures New.
type Option func(*options)

// SetSigningMethod sets the HMAC method used to sign tokens. The default is
// HS512.
func SetSigningMethod(method jwt.SigningMethod) Option {
	return func(o *options) {
		o.signingMethod = method
	}
}

// SetSigningKey sets the signing key. Tokens signed with oldKey are still
// accepted, so the key can be rotated without logging everyone out.
func SetSigningKey(key, oldKey string) Option {
	return func(o *options) {
		o.signingKey = []byte(key)
		if oldKey != "" && key != oldKey {
			o.signingKey2 = []byte(oldKey)
		}
	}
}

// SetExpired sets the token lifetime in seconds. The default is 7200.
func SetExpired(expired int) Option {
	return func(o *options) {
		o.expired = expired
	}
}

// New returns an Authenticator that signs tokens with HMAC and records
// revoked tokens in store. store may be nil, which disables revocation.
func New(store Storer, opts ...Option) Authenticator {
	o := options{
		tokenType:     "Bearer",
		expired:       7200,
		signingMethod: jwt.SigningMethodHS512,
	}

	for _, opt := range opts {
		opt(&o)
	}

	// There is deliberately no built-in default key: a key shipped in source
	// code lets anyone forge tokens. Without a key, signing and parsing fail.
	o.keyFuncs = append(o.keyFuncs, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		if len(o.signingKey) == 0 {
			return nil, ErrMissingSigningKey
		}
		return o.signingKey, nil
	})

	if o.signingKey2 != nil {
		o.keyFuncs = append(o.keyFuncs, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, ErrInvalidToken
			}
			return o.signingKey2, nil
		})
	}

	return &JWTAuth{
		opts:  &o,
		store: store,
	}
}

// JWTAuth is the Authenticator returned by New.
type JWTAuth struct {
	opts  *options
	store Storer
}

// Claims are the JWT claims issued by JWTAuth.
type Claims struct {
	jwt.RegisteredClaims
	// AuthTime is when the user last authenticated with credentials. Tokens
	// issued by a refresh keep the original value, so the age of a session
	// can be bounded independently of individual token lifetimes.
	AuthTime *jwt.NumericDate `json:"auth_time,omitempty"`
}

// SessionStart returns when the session began: AuthTime, or the issue time
// for tokens issued before AuthTime existed.
func (c *Claims) SessionStart() time.Time {
	if c.AuthTime != nil {
		return c.AuthTime.Time
	}
	if c.IssuedAt != nil {
		return c.IssuedAt.Time
	}
	return time.Time{}
}

type tokenOptions struct {
	authTime time.Time
}

// TokenOption customizes a generated token.
type TokenOption func(*tokenOptions)

// WithAuthTime sets the auth_time claim; by default it is the issue time.
func WithAuthTime(t time.Time) TokenOption {
	return func(o *tokenOptions) { o.authTime = t }
}

// GenerateToken issues a token for subject.
func (a *JWTAuth) GenerateToken(ctx context.Context, subject string, opts ...TokenOption) (TokenInfo, error) {
	if len(a.opts.signingKey) == 0 {
		return nil, ErrMissingSigningKey
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(a.opts.expired) * time.Second)
	o := tokenOptions{authTime: now}
	for _, opt := range opts {
		opt(&o)
	}

	token := jwt.NewWithClaims(a.opts.signingMethod, &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now),
			Subject:   subject,
		},
		// Whole seconds round-trip exactly through the float encoding;
		// millisecond values lose up to 1 ms per refresh.
		AuthTime: jwt.NewNumericDate(o.authTime.Truncate(time.Second)),
	})

	tokenStr, err := token.SignedString(a.opts.signingKey)
	if err != nil {
		return nil, err
	}

	tokenInfo := &tokenInfo{
		ExpiresAt:   expiresAt.Unix(),
		TokenType:   a.opts.tokenType,
		AccessToken: tokenStr,
	}
	return tokenInfo, nil
}

func (a *JWTAuth) parseToken(tokenStr string) (*Claims, error) {
	var (
		token *jwt.Token
		err   error
	)

	for _, keyFunc := range a.opts.keyFuncs {
		token, err = jwt.ParseWithClaims(tokenStr, &Claims{}, keyFunc,
			jwt.WithExpirationRequired(), jwt.WithIssuedAt())
		if err != nil || token == nil || !token.Valid {
			continue
		}
		break
	}

	if err != nil || token == nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	return token.Claims.(*Claims), nil
}

func (a *JWTAuth) callStore(fn func(Storer) error) error {
	if store := a.store; store != nil {
		return fn(store)
	}
	return nil
}

// DestroyToken revokes tokenStr until it expires.
func (a *JWTAuth) DestroyToken(ctx context.Context, tokenStr string) error {
	claims, err := a.parseToken(tokenStr)
	if err != nil {
		return err
	}

	return a.callStore(func(store Storer) error {
		expired := time.Until(claims.ExpiresAt.Time)
		return store.Set(ctx, tokenStr, expired)
	})
}

// ParseSubject validates tokenStr and returns its subject.
func (a *JWTAuth) ParseSubject(ctx context.Context, tokenStr string) (string, error) {
	claims, err := a.ParseClaims(ctx, tokenStr)
	if err != nil {
		return "", err
	}
	return claims.Subject, nil
}

// ParseClaims validates tokenStr, including the revocation store, and returns
// its claims.
func (a *JWTAuth) ParseClaims(ctx context.Context, tokenStr string) (*Claims, error) {
	if tokenStr == "" {
		return nil, ErrInvalidToken
	}

	claims, err := a.parseToken(tokenStr)
	if err != nil {
		return nil, err
	}

	err = a.callStore(func(store Storer) error {
		if exists, err := store.Check(ctx, tokenStr); err != nil {
			return err
		} else if exists {
			return ErrInvalidToken
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// Release closes the revocation store.
func (a *JWTAuth) Release(ctx context.Context) error {
	return a.callStore(func(store Storer) error {
		return store.Close(ctx)
	})
}
