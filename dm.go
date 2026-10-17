package x

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SendDM sends a direct message in an existing conversation.
func (c *Client) SendDM(ctx context.Context, conversationID, text string) (*Message, error) {
	if conversationID == "" || text == "" {
		return nil, classifyWriteOutcome(ErrInvalidParams)
	}

	payload := map[string]interface{}{
		"conversation_id":                 conversationID,
		"recipient_ids":                   false,
		"request_id":                      fmt.Sprintf("%d", time.Now().UnixNano()),
		"text":                            text,
		"cards_platform":                  "Web-12",
		"include_cards":                   1,
		"include_quote_count":             true,
		"dm_secret_conversations_enabled": false,
	}

	data, err := c.restPOST(ctx, "/i/api/1.1/dm/new2.json", payload)
	if err != nil {
		return nil, classifyWriteOutcome(err)
	}
	message, err := parseSentMessage(data, conversationID)
	if err != nil {
		return nil, classifyWriteOutcome(err)
	}
	return message, nil
}

// SendNewDM sends a direct message to a user by their ID, creating a new
// conversation if one doesn't exist. Returns ErrDMClosed if the recipient
// has DMs disabled.
//
// Payload matches X web / twikit: conversation_id is "<recipient>-<self>" and
// recipient_ids is false (the peer is implied by conversation_id). Passing the
// raw recipient id string as recipient_ids makes new2 return HTTP 400.
func (c *Client) SendNewDM(ctx context.Context, recipientID, text string) (*Message, error) {
	if recipientID == "" || text == "" {
		return nil, classifyWriteOutcome(ErrInvalidParams)
	}
	selfID := strings.TrimSpace(c.restID)
	if selfID == "" && c.viewer != nil {
		selfID = strings.TrimSpace(c.viewer.ID)
	}
	if selfID == "" {
		return nil, classifyWriteOutcome(fmt.Errorf("%w: authenticated user id unavailable for DM", ErrInvalidParams))
	}

	convID := recipientID + "-" + selfID
	payload := map[string]interface{}{
		"conversation_id":     convID,
		"recipient_ids":       false,
		"text":                text,
		"cards_platform":      "Web-12",
		"include_cards":       1,
		"include_quote_count": true,
		"dm_users":            false,
	}

	data, err := c.restPOST(ctx, "/i/api/1.1/dm/new2.json", payload)
	if err != nil {
		return nil, classifyWriteOutcome(err)
	}

	message, err := parseSentMessage(data, buildConversationID(selfID, recipientID))
	if err != nil {
		return nil, classifyWriteOutcome(err)
	}
	return message, nil
}

// buildConversationID creates the canonical conversation ID for a 1:1 DM.
// X always puts the lower numeric ID first.
func buildConversationID(id1, id2 string) string {
	if id1 > id2 {
		return id2 + "-" + id1
	}
	return id1 + "-" + id2
}

// GetConversations returns the user's DM conversations.
func (c *Client) GetConversations(ctx context.Context) (ConversationPage, error) {
	data, err := c.restGET(ctx, "/i/api/1.1/dm/inbox_initial_state.json", nil)
	if err != nil {
		return ConversationPage{}, err
	}
	return parseConversations(data)
}

// GetConversation returns messages from a specific DM conversation.
func (c *Client) GetConversation(ctx context.Context, conversationID string) (MessagePage, error) {
	if conversationID == "" {
		return MessagePage{}, ErrInvalidParams
	}
	if strings.ContainsAny(conversationID, "/.\\") {
		return MessagePage{}, fmt.Errorf("%w: conversationID contains invalid characters", ErrInvalidParams)
	}
	path := "/i/api/1.1/dm/conversation/" + conversationID + ".json"
	data, err := c.restGET(ctx, path, nil)
	if err != nil {
		return MessagePage{}, err
	}
	return parseMessages(data)
}

func parseSentMessage(data json.RawMessage, conversationID string) (*Message, error) {
	var resp struct {
		Entries []struct {
			Message struct {
				Data struct {
					ID        string `json:"id"`
					Text      string `json:"text"`
					SenderID  string `json:"sender_id"`
					CreatedAt string `json:"time"`
				} `json:"message_data"`
			} `json:"message"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("%w: parsing DM response: %v", ErrRequestFailed, err)
	}
	if len(resp.Entries) == 0 {
		return nil, fmt.Errorf("%w: no message in DM response", ErrRequestFailed)
	}
	entry := resp.Entries[0].Message.Data
	ts, _ := strconv.ParseInt(entry.CreatedAt, 10, 64)
	return &Message{
		ID:             entry.ID,
		ConversationID: conversationID,
		SenderID:       entry.SenderID,
		Text:           entry.Text,
		CreatedAt:      time.UnixMilli(ts),
	}, nil
}
