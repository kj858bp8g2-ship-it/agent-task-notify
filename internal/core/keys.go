package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Key hashes the JSON representation of a string array to avoid delimiter ambiguity.
func Key(parts ...string) string {
	if parts == nil {
		parts = []string{}
	}
	encoded, _ := json.Marshal(parts)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func ValidKey(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
