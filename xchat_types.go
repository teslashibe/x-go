package x

import "time"

// XChatConversation is a provider-observed one-to-one membership. The private
// seal prevents page reads after callers change the public identity fields or
// transfer membership to another client/session. It is not product authorization.
type XChatConversation struct {
	ID                         string   `json:"id"`
	SenderID                   string   `json:"sender_id"`
	PeerID                     string   `json:"peer_id"`
	ParticipantIDs             []string `json:"participant_ids"`
	client                     *Client
	conversation, sender, peer string
}

// XChatPageOptions carries the provider's sequence cursor, not a message UUID.
// Empty Cursor requests the newest page using the source-backed upper bound.
type XChatPageOptions struct{ Cursor string }

// XChatMessage retains distinct backend sequence and native message identities.
// RecipientID is derived only from verified one-to-one participants. Verified
// stays false: authenticated delivery/signature presence is not crypto proof.
type XChatMessage struct {
	SequenceID         string    `json:"sequence_id"`
	MessageID          string    `json:"message_id"`
	SenderID           string    `json:"sender_id"`
	RecipientID        string    `json:"recipient_id,omitempty"`
	ConversationID     string    `json:"conversation_id"`
	PreviousSequenceID string    `json:"previous_sequence_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	Kind               string    `json:"kind"`
	Text               string    `json:"text,omitempty"`
	Encrypted          bool      `json:"encrypted"`
	SignaturePresent   bool      `json:"signature_present"`
	Verified           bool      `json:"verified"`
	Attachments        bool      `json:"attachments"`
}

type XChatMessagePage struct {
	Events                []XChatMessage `json:"events"`
	NextCursor            string         `json:"next_cursor,omitempty"`
	HasMore               bool           `json:"has_more"`
	Coverage              DMReadCoverage `json:"coverage"`
	SupplementalKeyEvents int            `json:"supplemental_key_events"`
}
