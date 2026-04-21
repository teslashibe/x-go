package x

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SendDM sends a direct message in an existing conversation.
func (c *Client) SendDM(ctx context.Context, conversationID, text string) (*Message, error) {
	if conversationID == "" || text == "" {
		return nil, ErrInvalidParams
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
		return nil, err
	}
	return parseSentMessage(data, conversationID)
}

// SendNewDM sends a direct message to a user by their ID, creating a new
// conversation if one doesn't exist. Returns ErrDMClosed if the recipient
// has DMs disabled.
func (c *Client) SendNewDM(ctx context.Context, recipientID, text string) (*Message, error) {
	if recipientID == "" || text == "" {
		return nil, ErrInvalidParams
	}

	payload := map[string]interface{}{
		"conversation_id":                 recipientID + "-" + c.restID,
		"recipient_ids":                   recipientID,
		"request_id":                      fmt.Sprintf("%d", time.Now().UnixNano()),
		"text":                            text,
		"cards_platform":                  "Web-12",
		"include_cards":                   1,
		"include_quote_count":             true,
		"dm_secret_conversations_enabled": false,
		"dm_users":                        false,
	}

	data, err := c.restPOST(ctx, "/i/api/1.1/dm/new2.json", payload)
	if err != nil {
		return nil, err
	}

	convID := buildConversationID(c.restID, recipientID)
	return parseSentMessage(data, convID)
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

func parseConversations(data json.RawMessage) (ConversationPage, error) {
	var resp struct {
		InboxInitialState struct {
			Conversations map[string]struct {
				ConversationID  string `json:"conversation_id"`
				Type            string `json:"type"`
				Trusted         bool   `json:"trusted"`
				LastReadEventID string `json:"last_read_event_id"`
			} `json:"conversations"`
			Cursor string `json:"cursor"`
		} `json:"inbox_initial_state"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return ConversationPage{}, fmt.Errorf("%w: parsing conversations: %v", ErrRequestFailed, err)
	}

	page := ConversationPage{}
	for _, conv := range resp.InboxInitialState.Conversations {
		page.Conversations = append(page.Conversations, Conversation{
			ID:              conv.ConversationID,
			Type:            conv.Type,
			Trusted:         conv.Trusted,
			LastReadEventID: conv.LastReadEventID,
		})
	}
	sort.Slice(page.Conversations, func(i, j int) bool {
		return page.Conversations[i].ID < page.Conversations[j].ID
	})
	if resp.InboxInitialState.Cursor != "" {
		page.NextCursor = resp.InboxInitialState.Cursor
		page.HasNext = true
	}
	return page, nil
}

func parseMessages(data json.RawMessage) (MessagePage, error) {
	var resp struct {
		ConversationTimeline struct {
			Entries []struct {
				Message struct {
					Data struct {
						ID             string `json:"id"`
						Text           string `json:"text"`
						SenderID       string `json:"sender_id"`
						ConversationID string `json:"conversation_id"`
						CreatedAt      string `json:"time"`
					} `json:"message_data"`
				} `json:"message"`
			} `json:"entries"`
			MinEntryID string `json:"min_entry_id"`
			Status     string `json:"status"`
		} `json:"conversation_timeline"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return MessagePage{}, fmt.Errorf("%w: parsing messages: %v", ErrRequestFailed, err)
	}

	page := MessagePage{}
	for _, entry := range resp.ConversationTimeline.Entries {
		d := entry.Message.Data
		if d.ID == "" {
			continue
		}
		ts, _ := strconv.ParseInt(d.CreatedAt, 10, 64)
		page.Messages = append(page.Messages, Message{
			ID:             d.ID,
			ConversationID: d.ConversationID,
			SenderID:       d.SenderID,
			Text:           d.Text,
			CreatedAt:      time.UnixMilli(ts),
		})
	}
	if resp.ConversationTimeline.MinEntryID != "" && resp.ConversationTimeline.Status == "HAS_MORE" {
		page.NextCursor = resp.ConversationTimeline.MinEntryID
		page.HasNext = true
	}
	return page, nil
}
