// Package json wraps encoding/json.
package json

import (
	"encoding/json"
	"fmt"
)

// Aliases of encoding/json.
var (
	Marshal       = json.Marshal
	Unmarshal     = json.Unmarshal
	MarshalIndent = json.MarshalIndent
	NewDecoder    = json.NewDecoder
	NewEncoder    = json.NewEncoder
)

// MarshalToString encodes v as JSON, or returns "" if v cannot be encoded.
func MarshalToString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Println("Failed to marshal json string: " + err.Error())
		return ""
	}
	return string(b)
}
