package assets

import _ "embed"

//go:embed agent-icons.json
var agentIconsJSON []byte

// AgentIconsJSON returns a caller-owned copy of the packaged agent catalog.
func AgentIconsJSON() []byte { return append([]byte(nil), agentIconsJSON...) }
