package middleware

import (
	"net/url"
	"strings"
	"testing"
)

func TestRedactedURI(t *testing.T) {
	tests := map[string]string{
		"/api/v1/users":                               "/api/v1/users",
		"/api/v1/users?current=1":                     "/api/v1/users?current=1",
		"/download?accessToken=secret.jwt.value&id=7": "/download?accessToken=%2A%2A%2A%2A%2A%2A&id=7",
		"/x?access_token=s&token=t":                   "/x?access_token=%2A%2A%2A%2A%2A%2A&token=%2A%2A%2A%2A%2A%2A",
	}
	for in, want := range tests {
		u, err := url.Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		got := redactedURI(u)
		if got != want {
			t.Errorf("redactedURI(%q) = %q, want %q", in, got, want)
		}
		if strings.Contains(got, "secret") {
			t.Errorf("token leaked: %q", got)
		}
	}
}
