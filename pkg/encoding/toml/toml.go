// Package toml wraps the TOML library used to read configuration files.
package toml

import "github.com/BurntSushi/toml"

// Aliases for the underlying library.
var (
	Unmarshal  = toml.Unmarshal
	DecodeFile = toml.DecodeFile
	Decode     = toml.Decode
)

// Value is a TOML value whose decoding is deferred.
type Value = toml.Primitive
