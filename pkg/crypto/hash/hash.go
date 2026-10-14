// Package hash provides the digests and password hashing used by the service.
package hash

import (
	"crypto/md5" //nolint:gosec // MD5 is not used for security, see MD5.
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

// MD5 returns the hex MD5 digest of b.
//
// It is not a security primitive here: clients send MD5(password) as the
// wire format of a password, which is then stored with bcrypt, and MD5 also
// shortens values used as cache keys.
func MD5(b []byte) string {
	sum := md5.Sum(b) //nolint:gosec // See the function comment.
	return hex.EncodeToString(sum[:])
}

// MD5String returns the hex MD5 digest of s.
func MD5String(s string) string {
	return MD5([]byte(s))
}

// GeneratePassword returns the bcrypt hash of password.
func GeneratePassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CompareHashAndPassword returns nil if password matches the bcrypt hash
// hashedPassword.
func CompareHashAndPassword(hashedPassword, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
