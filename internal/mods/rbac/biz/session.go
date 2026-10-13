package biz

import (
	"context"
	"strconv"
	"time"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/pkg/cachex"
	"github.com/GoRustC/new-goadmin/pkg/crypto/hash"
	"github.com/GoRustC/new-goadmin/pkg/errors"
)

const (
	cacheNSLoginFailures = "login-fail"
	cacheNSTokenRevoked  = "token-revoked"
)

// dummyPasswordHash is compared against when a login names an unknown user,
// so the response takes as long as for a known user with a wrong password
// and timing does not reveal which accounts exist.
var dummyPasswordHash = func() string {
	h, err := hash.GeneratePassword("timing-equalizer")
	if err != nil {
		panic(err)
	}
	return h
}()

// loginLock counts failed logins per submitted account name.
type loginLock struct {
	cache cachex.Cacher
	cfg   *config.Config
}

func (l loginLock) key(username string) string {
	// Hash to bound the key length; names are not validated before this.
	return hash.MD5String(username)
}

func (l loginLock) failures(ctx context.Context, username string) (int, error) {
	val, ok, err := l.cache.Get(ctx, cacheNSLoginFailures, l.key(username))
	if err != nil || !ok {
		return 0, err
	}
	n, _ := strconv.Atoi(val)
	return n, nil
}

// check returns a 429 error while the name is locked.
func (l loginLock) check(ctx context.Context, username string) error {
	max := l.cfg.General.LoginLock.MaxFailures
	if max <= 0 {
		return nil
	}
	n, err := l.failures(ctx, username)
	if err != nil {
		return err
	}
	if n >= max {
		return errors.TooManyRequests(config.ErrLoginLockedID,
			"Too many failed login attempts, try again in %d minutes", l.cfg.General.LoginLock.LockMinutes)
	}
	return nil
}

// fail records a failed attempt. The read-modify-write is not atomic, so
// concurrent failures may be undercounted slightly; the captcha already
// limits how fast attempts can be made.
func (l loginLock) fail(ctx context.Context, username string) error {
	if l.cfg.General.LoginLock.MaxFailures <= 0 {
		return nil
	}
	n, err := l.failures(ctx, username)
	if err != nil {
		return err
	}
	return l.cache.Set(ctx, cacheNSLoginFailures, l.key(username), strconv.Itoa(n+1),
		time.Duration(l.cfg.General.LoginLock.LockMinutes)*time.Minute)
}

func (l loginLock) reset(ctx context.Context, username string) error {
	return l.cache.Delete(ctx, cacheNSLoginFailures, l.key(username))
}

// revokeUserTokens invalidates every token issued to userID before now. The
// record lives as long as a token can, after which old tokens have expired.
func revokeUserTokens(ctx context.Context, cache cachex.Cacher, cfg *config.Config, userID string) error {
	return cache.Set(ctx, cacheNSTokenRevoked, userID, strconv.FormatInt(time.Now().UnixMilli(), 10),
		time.Duration(cfg.Middleware.Auth.Expired)*time.Second)
}

// tokenRevoked reports whether a token issued at issuedAt predates the
// user's last revocation.
func tokenRevoked(ctx context.Context, cache cachex.Cacher, userID string, issuedAt time.Time) (bool, error) {
	val, ok, err := cache.Get(ctx, cacheNSTokenRevoked, userID)
	if err != nil || !ok {
		return false, err
	}
	revokedAt, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return false, nil
	}
	return issuedAt.UnixMilli() < revokedAt, nil
}
