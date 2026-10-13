package schema

import "testing"

func TestMenuResourceValidate(t *testing.T) {
	valid := []MenuResource{
		{Method: "GET", Path: "/api/v1/users"},
		{Method: "PATCH", Path: "/api/v1/users/{id}/reset-pwd"},
		{Method: "DELETE", Path: "/api/v1/users/:id"},
		{Method: "GET", Path: "/api/v1/files/*"},
	}
	for _, r := range valid {
		if err := r.Validate(); err != nil {
			t.Errorf("%s %s should be valid: %v", r.Method, r.Path, err)
		}
	}
	invalid := []MenuResource{
		{Method: "GET", Path: "/x\np, role1, /api/v1/*, DELETE"}, // policy injection
		{Method: "GET", Path: "/a,b"},                            // extra CSV field
		{Method: "GET", Path: "/a b"},
		{Method: "GET", Path: `/a"b`},
		{Method: "GET", Path: "api/v1/users"}, // must be absolute
		{Method: "get", Path: "/api/v1/users"},
		{Method: "GET\n", Path: "/api/v1/users"},
		{Method: "TRACE", Path: "/api/v1/users"},
	}
	for _, r := range invalid {
		if err := r.Validate(); err == nil {
			t.Errorf("%q %q should be rejected", r.Method, r.Path)
		}
	}
}
