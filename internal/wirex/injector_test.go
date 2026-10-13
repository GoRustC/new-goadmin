package wirex

import (
	"context"
	"strings"
	"testing"

	"github.com/GoRustC/new-goadmin/internal/config"
)

func TestResolveSigningKeys(t *testing.T) {
	const goodKey = "0123456789abcdef0123456789abcdef"

	tests := []struct {
		name    string
		debug   bool
		key     string
		oldKey  string
		wantErr bool
	}{
		{name: "valid key", key: goodKey},
		{name: "empty key outside debug", key: "", wantErr: true},
		{name: "empty key in debug", key: "", debug: true},
		{name: "short key", key: "short", wantErr: true},
		{name: "known default key", key: "XnEsT0S@", debug: true, wantErr: true},
		{name: "known default old key", key: goodKey, oldKey: "CG24SDVP8OHPK395GB5G", wantErr: true},
	}

	orig := config.C.General.Debug
	t.Cleanup(func() { config.C.General.Debug = orig })

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.C.General.Debug = tt.debug
			key, _, err := resolveSigningKeys(context.Background(), tt.key, tt.oldKey)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if strings.ToLower(err.Error()[:1]) != err.Error()[:1] {
					t.Errorf("error string should start lowercase: %q", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(key) < minSigningKeyLen {
				t.Fatalf("resolved key too short: %d bytes", len(key))
			}
		})
	}
}

func TestResolveSigningKeysRandomPerCall(t *testing.T) {
	orig := config.C.General.Debug
	t.Cleanup(func() { config.C.General.Debug = orig })
	config.C.General.Debug = true

	a, _, err := resolveSigningKeys(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := resolveSigningKeys(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("generated debug keys should differ between runs")
	}
}
