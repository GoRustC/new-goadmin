package util

import (
	"github.com/google/uuid"
	"github.com/rs/xid"
)

// NewXID returns a new globally unique, time-sortable 20-character ID.
func NewXID() string {
	return xid.New().String()
}

// MustNewUUID returns a new random (version 4) UUID. It panics if the system
// random source fails.
func MustNewUUID() string {
	v, err := uuid.NewRandom()
	if err != nil {
		panic(err)
	}
	return v.String()
}
