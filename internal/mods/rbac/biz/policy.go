package biz

import "context"

// PolicyReloader rebuilds the authorization policy from the database. Role
// and menu changes call it right after they commit, so they take effect
// immediately; the service runs as a single instance and needs no other
// signal.
type PolicyReloader interface {
	Reload(ctx context.Context) error
}
