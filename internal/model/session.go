package model

// SessionContext captures session metadata from hive-local request headers.
type SessionContext struct {
	SessionKey    string `json:"session_key,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	Channel       string `json:"channel,omitempty"`
	SenderID      string `json:"sender_id,omitempty"`
	SenderIsOwner bool   `json:"sender_is_owner,omitempty"`
	Sandboxed     bool   `json:"sandboxed,omitempty"`
}
