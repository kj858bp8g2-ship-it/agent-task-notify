package core

// Event is the minimal normalized hook event. It deliberately excludes hook text.
type Event struct {
	AgentID, SessionID, NativeRunID, EventType, Reason string
	IsChild                                            bool
}
