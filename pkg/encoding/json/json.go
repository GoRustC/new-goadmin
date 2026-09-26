package json

import (
	"encoding/json"
	"fmt"
)

var (
	Marshal       = json.Marshal
	Unmarshal     = json.Unmarshal
	MarshalIndent = json.MarshalIndent
	NewDecoder    = json.NewDecoder
	NewEncoder    = json.NewEncoder
)

func MarshalToString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Println("Failed to marshal json string: " + err.Error())
		return ""
	}
	return string(b)
}
