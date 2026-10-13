package middleware

import "testing"

func TestMatchPathPrefix(t *testing.T) {
	tests := []struct {
		path, prefix string
		want         bool
	}{
		{"/api/v1/login", "/api/v1/login", true},
		{"/api/v1/login/", "/api/v1/login", true},
		{"/api/v1/login/sso", "/api/v1/login", true},
		{"/api/v1/login-history", "/api/v1/login", false}, // used to be exempted
		{"/api/v1/logins", "/api/v1/login", false},
		{"/api/v1/captcha/id", "/api/v1/captcha/", true},
		{"/api/v1/captcha", "/api/v1/captcha/", false},
		{"/api/v1/current/user", "/api/v1/current/", true},
		{"/api/v1/currentx", "/api/v1/current/", false},
		{"/api/v1/users", "/api/", true},
		{"/apix", "/api/", false},
		{"/health", "/api/", false},
	}
	for _, tt := range tests {
		if got := matchPathPrefix(tt.path, tt.prefix); got != tt.want {
			t.Errorf("matchPathPrefix(%q, %q) = %v, want %v", tt.path, tt.prefix, got, tt.want)
		}
	}
}
