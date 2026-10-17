package x

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DMPageOptions carries the provider continuation token without interpreting it.
type DMPageOptions struct{ Cursor string }

// DMInboxPageOptions keeps historical folder pagination separate from updates.
// Empty options request initial state; continuation requires both fields.
type DMInboxPageOptions struct {
	Folder string
	Cursor string
}
type DMFolderHistory struct {
	Cursor string `json:"cursor,omitempty"`
	Status string `json:"status"`
}

// GetInboxPage performs exactly one initial or folder-history request.
// Request evidence: tweety f6bfc8610bd3ec8af2fe000076bc1b53a6327e7d,
// src/tweety/builder.py get_trusted_inbox/get_untrusted_inbox.
func (c *Client) GetInboxPage(ctx context.Context, opts DMInboxPageOptions) (ConversationPage, error) {
	if opts.Folder == "" && opts.Cursor == "" {
		page, err := c.GetConversations(ctx)
		page.NextCursor = ""
		page.HasNext = false
		return page, err
	}
	if (opts.Folder != "trusted" && opts.Folder != "untrusted") || !dmNumericID(opts.Cursor) {
		return ConversationPage{}, ErrInvalidParams
	}
	q := url.Values{"max_id": {opts.Cursor}, "count": {"100"}, "include_inbox_timelines": {"true"}, "include_groups": {"true"}, "filter_low_quality": {"true"}, "include_quality": {"all"}, "dm_secret_conversations_enabled": {"false"}}
	if opts.Folder == "untrusted" {
		q.Set("include_quality", "high")
	}
	data, err := c.restGET(ctx, "/i/api/1.1/dm/inbox_timeline/"+opts.Folder+".json", q)
	if err != nil {
		return ConversationPage{}, err
	}
	body, err := dmEnvelope(data, "inbox_timeline")
	if err != nil {
		return ConversationPage{}, err
	}
	wrapped, _ := json.Marshal(map[string]any{"inbox_initial_state": body})
	page, err := parseConversations(wrapped)
	if err != nil {
		return ConversationPage{}, err
	}
	var status, cursor string
	if raw, ok := body["status"]; ok {
		if json.Unmarshal(raw, &status) != nil {
			return ConversationPage{}, dmReadError("invalid_status")
		}
	}
	if raw, ok := body["min_entry_id"]; ok {
		if json.Unmarshal(raw, &cursor) != nil {
			return ConversationPage{}, dmReadError("invalid_cursor")
		}
	}
	if status == "HAS_MORE" && !dmNumericID(cursor) {
		return ConversationPage{}, dmReadError("missing_cursor")
	}
	page.UpdateCursor = ""
	page.NextCursor = cursor
	page.HasNext = status == "HAS_MORE"
	if cursor != "" {
		page.Coverage.CursorState = "present"
	} else if status == "AT_END" {
		page.Coverage.CursorState = "exhausted_unverified"
	}
	page.InboxHistory = map[string]DMFolderHistory{opts.Folder: {Cursor: cursor, Status: status}}
	return page, nil
}

// DMReadCoverage describes this response only; supported does not establish a
// complete mailbox or history window. Pagination remains unverified.
type DMReadCoverage struct {
	// Membership is unverified until the inbox schema and supported conversation types are live-validated.
	Membership string `json:"membership"`
	// CursorState is present, absent_unverified, or exhausted_unverified.
	CursorState         string `json:"cursorState"`
	State               string `json:"state"`
	Reason              string `json:"reason,omitempty"`
	PaginationSupported bool   `json:"paginationSupported"`
	UnsupportedEntries  int    `json:"unsupportedEntries,omitempty"`
}

// DMAttachment preserves the existence of content the SDK cannot decode.
// It does not download media or expose the raw private provider payload.
type DMAttachment struct {
	Kind      string `json:"kind"`
	Supported bool   `json:"supported"`
}

// GetConversationsPage reads one inbox page. Continuation is deliberately
// rejected until the provider's continuation request contract is verified.
func (c *Client) GetConversationsPage(ctx context.Context, options DMPageOptions) (ConversationPage, error) {
	if options.Cursor != "" {
		return ConversationPage{}, fmt.Errorf("%w: DM inbox continuation unverified", ErrInvalidParams)
	}
	return c.GetConversations(ctx)
}

// GetConversationPage reads one history page, preserving existing first-page behavior.
func (c *Client) GetConversationPage(ctx context.Context, conversationID string, options DMPageOptions) (MessagePage, error) {
	if !dmConversationID(conversationID) {
		return MessagePage{}, ErrInvalidParams
	}
	if options.Cursor == "" {
		page, err := c.GetConversation(ctx, conversationID)
		if err != nil {
			return MessagePage{}, err
		}
		for _, m := range page.Messages {
			if m.ConversationID != conversationID {
				return MessagePage{}, dmReadError("conflicting_requested_conversation")
			}
		}
		return page, nil
	}
	if !dmNumericID(options.Cursor) {
		return MessagePage{}, ErrInvalidParams
	}
	// twikit c3b7220866f8582009fe2d1155b6fe92192a2711 client/v11.py
	// dm_conversation; twitter-api-client c150f1a3492ce3db15b954f2bc18b4976500a73b
	// account.py dm_history passes min_entry_id unchanged as max_id.
	data, err := c.restGET(ctx, "/i/api/1.1/dm/conversation/"+conversationID+".json", url.Values{"context": {"FETCH_DM_CONVERSATION_HISTORY"}, "include_conversation_info": {"true"}, "max_id": {options.Cursor}})
	if err != nil {
		return MessagePage{}, err
	}
	page, err := parseMessages(data)
	if err != nil {
		return MessagePage{}, err
	}
	for _, m := range page.Messages {
		if m.ConversationID != conversationID {
			return MessagePage{}, dmReadError("conflicting_requested_conversation")
		}
	}
	return page, nil
}

func dmReadError(reason string) error {
	return fmt.Errorf("%w: DM response %s", ErrRequestFailed, reason)
}

func dmEnvelope(data json.RawMessage, key string) (map[string]json.RawMessage, error) {
	var outer map[string]json.RawMessage
	if json.Unmarshal(data, &outer) != nil {
		return nil, dmReadError("invalid_json")
	}
	var body map[string]json.RawMessage
	raw, ok := outer[key]
	if !ok || json.Unmarshal(raw, &body) != nil || body == nil {
		return nil, dmReadError("unsupported_envelope")
	}
	return body, nil
}

func parseConversations(data json.RawMessage) (ConversationPage, error) {
	body, err := dmEnvelope(data, "inbox_initial_state")
	if err != nil {
		return ConversationPage{}, err
	}
	var conversations map[string]struct {
		ConversationID  string `json:"conversation_id"`
		Type            string `json:"type"`
		Trusted         bool   `json:"trusted"`
		LastReadEventID string `json:"last_read_event_id"`
		Participants    []struct {
			UserID string `json:"user_id"`
		} `json:"participants"`
	}
	raw, ok := body["conversations"]
	if !ok || string(raw) == "null" || json.Unmarshal(raw, &conversations) != nil {
		return ConversationPage{}, dmReadError("invalid_conversations")
	}
	page := ConversationPage{Coverage: DMReadCoverage{State: "partial", Reason: "inbox_capability_unverified", Membership: "unverified", CursorState: "absent_unverified"}}
	for key, conv := range conversations {
		if conv.ConversationID == "" || conv.ConversationID != key {
			return ConversationPage{}, dmReadError("missing_conversation_identity")
		}
		participants := []User{}
		seen := map[string]bool{}
		for _, p := range conv.Participants {
			if !dmNumericID(p.UserID) || seen[p.UserID] {
				return ConversationPage{}, dmReadError("invalid_participant_identity")
			}
			seen[p.UserID] = true
			participants = append(participants, User{ID: p.UserID})
		}
		if conv.Type == "ONE_TO_ONE" && len(participants) != 2 {
			page.Coverage.Reason = "incomplete_participant_membership"
		}
		page.Conversations = append(page.Conversations, Conversation{ID: conv.ConversationID, Type: conv.Type, Trusted: conv.Trusted, LastReadEventID: conv.LastReadEventID, Participants: participants, Coverage: DMReadCoverage{State: "partial", Reason: "history_not_read", Membership: "unverified", CursorState: "absent_unverified"}})
	}
	// The head carries messages even when X already marked them read. Reuse the
	// history parser; the synthetic envelope changes no provider content.
	if entries, ok := body["entries"]; ok {
		envelope, _ := json.Marshal(map[string]any{"conversation_timeline": map[string]any{"entries": entries, "status": "INBOX_HEAD"}})
		parsed, err := parseMessages(envelope)
		if err != nil {
			return ConversationPage{}, err
		}
		page.Messages = parsed.Messages
		page.Coverage.UnsupportedEntries = parsed.Coverage.UnsupportedEntries
		for i := range page.Conversations {
			conv := &page.Conversations[i]
			if parsed.Coverage.UnsupportedEntries > 0 {
				conv.Coverage.Reason = "unsupported_event"
				conv.Coverage.UnsupportedEntries = parsed.Coverage.UnsupportedEntries
			}
			for j := range page.Messages {
				message := &page.Messages[j]
				if message.ConversationID != conv.ID {
					continue
				}
				if conv.LastMessage == nil || message.CreatedAt.After(conv.LastMessage.CreatedAt) || message.CreatedAt.Equal(conv.LastMessage.CreatedAt) && message.ID > conv.LastMessage.ID {
					conv.LastMessage = message
				}
				if message.EventAt.After(conv.LatestEventAt) || message.EventAt.Equal(conv.LatestEventAt) && message.ID > conv.LatestEventID {
					conv.LatestEventAt = message.EventAt
					conv.LatestEventID = message.ID
				}
				if len(message.Attachments) > 0 {
					conv.Coverage.Reason = "unsupported_attachment"
				}
			}
		}
	}
	sort.Slice(page.Conversations, func(i, j int) bool { return page.Conversations[i].ID < page.Conversations[j].ID })
	if raw, ok := body["cursor"]; ok {
		if json.Unmarshal(raw, &page.NextCursor) != nil {
			return ConversationPage{}, dmReadError("invalid_cursor")
		}
	}
	page.UpdateCursor = page.NextCursor
	var folders map[string]DMFolderHistory
	if raw, ok := body["inbox_timelines"]; ok {
		var wire map[string]struct {
			Cursor string `json:"min_entry_id"`
			Status string `json:"status"`
		}
		if json.Unmarshal(raw, &wire) != nil {
			return ConversationPage{}, dmReadError("invalid_inbox_timelines")
		}
		folders = map[string]DMFolderHistory{}
		for _, name := range []string{"trusted", "untrusted"} {
			if folder, ok := wire[name]; ok {
				if folder.Status == "HAS_MORE" && !dmNumericID(folder.Cursor) {
					return ConversationPage{}, dmReadError("missing_folder_cursor")
				}
				folders[name] = DMFolderHistory{Cursor: folder.Cursor, Status: folder.Status}
			}
		}
	}
	page.InboxHistory = folders
	page.HasNext = page.NextCursor != ""
	if page.HasNext {
		page.Coverage.CursorState = "present"
	}
	return page, nil
}

func parseMessages(data json.RawMessage) (MessagePage, error) {
	body, err := dmEnvelope(data, "conversation_timeline")
	if err != nil {
		return MessagePage{}, err
	}
	var entries []map[string]json.RawMessage
	raw, ok := body["entries"]
	if !ok || string(raw) == "null" || json.Unmarshal(raw, &entries) != nil {
		return MessagePage{}, dmReadError("invalid_entries")
	}
	page := MessagePage{Coverage: DMReadCoverage{State: "partial", Reason: "history_capability_unverified", Membership: "unverified", CursorState: "absent_unverified"}}
	for _, entry := range entries {
		raw, ok := entry["message"]
		if !ok {
			page.Coverage.State = "partial"
			page.Coverage.Reason = "unsupported_event"
			page.Coverage.UnsupportedEntries++
			continue
		}
		var message struct {
			ID             string `json:"id"`
			ConversationID string `json:"conversation_id"`
			Time           string `json:"time"`
			Data           struct {
				ID             string          `json:"id"`
				Text           string          `json:"text"`
				SenderID       string          `json:"sender_id"`
				RecipientID    string          `json:"recipient_id"`
				ConversationID string          `json:"conversation_id"`
				Time           string          `json:"time"`
				Attachment     json.RawMessage `json:"attachment"`
			} `json:"message_data"`
		}
		if json.Unmarshal(raw, &message) != nil {
			return MessagePage{}, dmReadError("invalid_message")
		}
		d := message.Data
		if !dmNumericID(d.ID) || !dmNumericID(d.SenderID) || message.ConversationID == "" || !dmNumericID(message.ID) {
			return MessagePage{}, dmReadError("missing_message_identity")
		}
		if d.ID != message.ID {
			return MessagePage{}, dmReadError("conflicting_message_identity")
		}
		if d.ConversationID != "" && d.ConversationID != message.ConversationID {
			return MessagePage{}, dmReadError("conflicting_conversation_identity")
		}
		if d.RecipientID != "" && !dmNumericID(d.RecipientID) {
			return MessagePage{}, dmReadError("invalid_recipient_identity")
		}
		outerTime, outerErr := strconv.ParseInt(message.Time, 10, 64)
		if outerErr != nil || outerTime <= 0 {
			return MessagePage{}, dmReadError("invalid_event_time")
		}
		ts, err := strconv.ParseInt(d.Time, 10, 64)
		if err != nil || ts <= 0 {
			return MessagePage{}, dmReadError("invalid_message_time")
		}
		m := Message{ID: d.ID, Text: d.Text, SenderID: d.SenderID, RecipientID: d.RecipientID, ConversationID: message.ConversationID, CreatedAt: time.UnixMilli(ts), EventAt: time.UnixMilli(outerTime), Kind: "human"}
		if len(d.Attachment) > 0 && string(d.Attachment) != "null" {
			m.Attachments = []DMAttachment{{Kind: "unknown", Supported: false}}
			page.Coverage.State = "partial"
			page.Coverage.Reason = "unsupported_attachment"
		}
		if d.Text == "" && len(m.Attachments) == 0 {
			page.Coverage.State = "partial"
			page.Coverage.Reason = "unsupported_content"
		}
		page.Messages = append(page.Messages, m)
	}
	var status string
	if raw, ok := body["status"]; ok {
		if json.Unmarshal(raw, &status) != nil {
			return MessagePage{}, dmReadError("invalid_status")
		}
	}
	if raw, ok := body["min_entry_id"]; ok {
		if json.Unmarshal(raw, &page.NextCursor) != nil {
			return MessagePage{}, dmReadError("invalid_cursor")
		}
	}
	page.HasNext = status == "HAS_MORE"
	if page.NextCursor != "" {
		page.Coverage.CursorState = "present"
	}
	if status == "AT_END" {
		page.Coverage.CursorState = "exhausted_unverified"
	}
	if page.HasNext && page.NextCursor == "" {
		return MessagePage{}, dmReadError("missing_cursor")
	}
	if !page.HasNext {
		page.NextCursor = ""
	}
	if status != "HAS_MORE" && status != "AT_END" {
		page.Coverage.State = "partial"
		page.Coverage.Reason = "unverified_timeline_status"
	}
	return page, nil
}

// Provider IDs remain strings so numeric snowflakes are never rounded.
func dmNumericID(v string) bool {
	if v == "" || v == "0" {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func dmConversationID(v string) bool {
	parts := strings.Split(v, "-")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !dmNumericID(part) {
			return false
		}
	}
	return true
}
