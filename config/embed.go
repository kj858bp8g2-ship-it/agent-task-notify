package config

import _ "embed"

//go:embed defaults.json
var defaultsJSON []byte

// DefaultsJSON returns a caller-owned copy of the packaged defaults.
func DefaultsJSON() []byte { return append([]byte(nil), defaultsJSON...) }
