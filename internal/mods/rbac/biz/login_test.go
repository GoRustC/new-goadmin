package biz

import (
	"testing"

	"github.com/LyricTian/gin-admin/v10/pkg/crypto/hash"
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
