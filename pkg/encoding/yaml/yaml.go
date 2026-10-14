// Package yaml wraps gopkg.in/yaml.v3.
package yaml

import (
	"gopkg.in/yaml.v3"
)

// Aliases of gopkg.in/yaml.v3.
var (
	Marshal    = yaml.Marshal
	Unmarshal  = yaml.Unmarshal
	NewDecoder = yaml.NewDecoder
	NewEncoder = yaml.NewEncoder
)
