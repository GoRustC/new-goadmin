package config

// CacheNSForUser is the cache namespace of per-user entries (util.UserCache).
const CacheNSForUser = "user"

// IDs of login and session errors, which a client can act on.
//
//nolint:gosec // Error IDs, not credentials.
const (
	ErrInvalidTokenID            = "com.invalid.token"
	ErrInvalidCaptchaID          = "com.invalid.captcha"
	ErrInvalidUsernameOrPassword = "com.invalid.username-or-password"
	ErrLoginLockedID             = "com.login.locked"
	ErrSessionExpiredID          = "com.session.expired"
)
