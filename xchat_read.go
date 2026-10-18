package x

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Protocol evidence (not implementation copied): mautrix/twitter commit
// 726c742bf21211649d3bca4382f5467d07fcd735, messaging.go makeXChatQueryRequest,
// data/endpoints/endpoints.go and data/payload/{form,json}.go. The API host is
// intentionally api.x.com, distinct from the legacy x.com/i/api endpoint.
const chatAPI = "https://api.x.com/graphql/"
const chatMembershipPath = "uQEDp5FgdqNiG2jT5q07Jw/GetInboxPageConversationDataRequestQuery"
const chatHistoryPath = "IVlXls9JTnbgQ1gxsGAfJA/GetConversationPageQuery"
const chatInitialSequence = "9223372036854775807"

func (c *Client) chatGET(ctx context.Context, path string, variables any) (json.RawMessage, error) {
	c.waitForGap(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vars, err := json.Marshal(variables)
	if err != nil {
		return nil, ErrInvalidParams
	}
	params := url.Values{"variables": {string(vars)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, chatAPI+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, ErrInvalidParams
	}
	// Protocol fact: maintained reference 726c742, messaging.go
	// makeXChatQueryRequest enables WithClientUUID; headers.go sends the
	// session's once-generated RFC4122 UUID. This is not an account identity.
	c.chatClientIDOnce.Do(func() {
		var id [16]byte
		if _, c.chatClientIDErr = rand.Read(id[:]); c.chatClientIDErr != nil {
			return
		}
		id[6] = (id[6] & 0x0f) | 0x40
		id[8] = (id[8] & 0x3f) | 0x80
		c.chatClientID = fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	})
	if c.chatClientIDErr != nil {
		return nil, fmt.Errorf("%w: XChat client initialization", ErrRequestFailed)
	}
	c.setHeaders(req)
	req.Header.Set("X-Client-UUID", c.chatClientID)
	req.Header.Set("Origin", "https://x.com")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	// A redirect would turn this explicit single read into another physical
	// request (and could forward cookies). Keep the same transport/proxy/Jar and
	// deadline while refusing redirects locally.
	httpClient := &http.Client{Transport: c.httpClient.Transport, Jar: c.httpClient.Jar, Timeout: c.httpClient.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: XChat read: %w", ErrRequestFailed, err)
	}
	defer resp.Body.Close()
	if err = c.checkStatus(resp); err != nil {
		return nil, c.operationError("XChatRead", req, resp, err)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("%w: XChat body: %w", ErrRequestFailed, err)
	}
	if len(body) > maxResponseBody {
		return nil, dmReadError("xchat_response_too_large")
	}
	envelope, err := decodeGQLResponse(body)
	if err != nil {
		return nil, err
	}
	// Unlike public timelines, private history cannot silently discard partial
	// GraphQL errors and then report supported coverage.
	if len(envelope.Errors) > 0 {
		return nil, classifyGQLError(envelope.Errors[0])
	}
	return readGQLData(body, envelope)
}

// GetXChatConversation performs exactly one cookie-authenticated membership GET.
// It never fetches keys, resolves users or marks a conversation read.
func (c *Client) GetXChatConversation(ctx context.Context, conversationID, senderID, peerID string) (XChatConversation, error) {
	if c.restID != "" && c.restID != senderID {
		return XChatConversation{}, ErrInvalidParams
	}
	if !chatConversationID(conversationID, senderID, peerID) || !dmNumericID(senderID) || !dmNumericID(peerID) || senderID == peerID {
		return XChatConversation{}, ErrInvalidParams
	}
	data, err := c.chatGET(ctx, chatMembershipPath, struct {
		Conversation string `json:"conversation_id"`
		Keys         bool   `json:"include_user_public_keys"`
	}{conversationID, false})
	if err != nil {
		return XChatConversation{}, err
	}
	var body struct {
		Result *struct {
			Data *struct {
				Detail *struct {
					ID           string                      `json:"conversation_id"`
					Group        json.RawMessage             `json:"group_metadata"`
					Participants []chatMembershipParticipant `json:"participants_results"`
				} `json:"conversation_detail"`
			} `json:"data"`
		} `json:"get_inbox_page_conversation_data"`
	}
	if json.Unmarshal(data, &body) != nil || body.Result == nil || body.Result.Data == nil || body.Result.Data.Detail == nil {
		return XChatConversation{}, dmReadError("xchat_missing_membership")
	}
	detail := body.Result.Data.Detail
	membershipError := func(reason string) error {
		return &DMReadError{Reason: reason, Membership: chatMembershipDiagnostics(detail.ID, detail.Group, detail.Participants, conversationID, senderID, peerID)}
	}
	if detail.ID != conversationID {
		return XChatConversation{}, membershipError("xchat_membership_cid_mismatch")
	}
	if len(detail.Group) > 0 && string(detail.Group) != "null" {
		return XChatConversation{}, membershipError("xchat_membership_group_present")
	}
	// X omits participants for some one-to-one chats. The canonical ID already
	// names exactly the sender and peer, and only the sender's session returned it.
	if len(detail.Participants) == 0 {
		return XChatConversation{ID: conversationID, SenderID: senderID, PeerID: peerID, ParticipantIDs: []string{senderID, peerID}, client: c, conversation: conversationID, sender: senderID, peer: peerID}, nil
	}
	if len(detail.Participants) != 2 {
		return XChatConversation{}, membershipError("xchat_membership_participant_count")
	}
	ids := make([]string, 0, 2)
	for _, p := range detail.Participants {
		id := p.ID
		if p.Result != nil {
			if id != "" && p.Result.ID != "" && id != p.Result.ID {
				return XChatConversation{}, membershipError("xchat_membership_participant_conflict")
			}
			if id == "" {
				id = p.Result.ID
			}
		}
		if !dmNumericID(id) {
			return XChatConversation{}, membershipError("xchat_missing_participant")
		}
		ids = append(ids, id)
	}
	if !(ids[0] == senderID && ids[1] == peerID || ids[1] == senderID && ids[0] == peerID) {
		return XChatConversation{}, membershipError("xchat_membership_pair_mismatch")
	}
	return XChatConversation{ID: conversationID, SenderID: senderID, PeerID: peerID, ParticipantIDs: ids, client: c, conversation: conversationID, sender: senderID, peer: peerID}, nil
}

// GetXChatConversationPage performs exactly one modern history GET. It requires
// membership issued by this client and never falls back or requests keys.
func (c *Client) GetXChatConversationPage(ctx context.Context, conv XChatConversation, options XChatPageOptions) (XChatMessagePage, error) {
	participantsMatch := len(conv.ParticipantIDs) == 2 && (conv.ParticipantIDs[0] == conv.sender && conv.ParticipantIDs[1] == conv.peer || conv.ParticipantIDs[1] == conv.sender && conv.ParticipantIDs[0] == conv.peer)
	if conv.client != c || conv.ID != conv.conversation || conv.SenderID != conv.sender || conv.PeerID != conv.peer || !participantsMatch || c.restID != "" && c.restID != conv.sender {
		return XChatMessagePage{}, ErrInvalidParams
	}
	cursor := options.Cursor
	if cursor == "" {
		cursor = chatInitialSequence
	}
	if !chatSequence(cursor) {
		return XChatMessagePage{}, ErrInvalidParams
	}
	variables := struct {
		Conversation string `json:"conversation_id"`
		Sequence     string `json:"min_local_sequence_id"`
		Key          string `json:"min_conversation_key_version"`
		Settings     struct {
			InboxEvent int `json:"inbox_conversation_event_limit"`
			Inbox      int `json:"inbox_conversation_limit"`
			Event      int `json:"conversation_event_limit"`
			User       int `json:"user_event_limit"`
		} `json:"query_settings"`
	}{Conversation: conv.ID, Sequence: cursor, Key: chatInitialSequence}
	variables.Settings.InboxEvent = 5
	variables.Settings.Inbox = 20
	variables.Settings.Event = 200
	variables.Settings.User = 500
	data, err := c.chatGET(ctx, chatHistoryPath, variables)
	if err != nil {
		return XChatMessagePage{}, err
	}
	var body struct {
		Page *struct {
			Events *[]string `json:"encoded_message_events"`
			Keys   []string  `json:"missing_conversation_key_change_events"`
			More   *bool     `json:"has_more"`
		} `json:"get_conversation_page"`
	}
	if json.Unmarshal(data, &body) != nil || body.Page == nil || body.Page.Events == nil || body.Page.More == nil {
		return XChatMessagePage{}, dmReadError("xchat_invalid_page")
	}
	if len(*body.Page.Events) > 1000 || len(body.Page.Keys) > 1000 {
		return XChatMessagePage{}, dmReadError("xchat_event_limit")
	}
	page := XChatMessagePage{HasMore: *body.Page.More, SupplementalKeyEvents: len(body.Page.Keys), Coverage: DMReadCoverage{State: "supported", Membership: "verified", PaginationSupported: true}}
	gap := func(reason string) {
		page.Coverage.State = "partial"
		page.Coverage.Reason = reason
		page.Coverage.UnsupportedEntries++
	}
	seen := map[string]bool{}
	messageIDs := map[string]bool{}
	for _, encoded := range *body.Page.Events {
		evt, decodeErr := decodeChatEvent(encoded)
		if decodeErr != nil {
			gap("xchat_invalid_event")
			continue
		}
		if !chatSequence(evt.SequenceID) {
			gap("xchat_invalid_sequence")
			continue
		}
		// The source advances with the minimum sequence itself, not minus one.
		// Do not presume undocumented inclusive boundary overlap: every returned
		// main-stream event must be strictly older, including terminal pages.
		if !chatSequenceLess(evt.SequenceID, cursor) {
			return XChatMessagePage{}, dmReadError("xchat_sequence_outside_page")
		}
		if page.NextCursor == "" || chatSequenceLess(evt.SequenceID, page.NextCursor) {
			page.NextCursor = evt.SequenceID
		}
		if seen[evt.SequenceID] {
			gap("xchat_duplicate_sequence")
			continue
		}
		seen[evt.SequenceID] = true
		if evt.ConversationID != conv.ID {
			return XChatMessagePage{}, dmReadError("xchat_conversation_mismatch")
		}
		if evt.Kind == "human" && (evt.SenderID != conv.SenderID && evt.SenderID != conv.PeerID) {
			return XChatMessagePage{}, dmReadError("xchat_sender_mismatch")
		}
		if evt.CreatedAt.IsZero() || evt.CreatedAt.UnixMilli() <= 0 || evt.CreatedAt.After(time.Now().Add(5*time.Minute)) {
			gap("xchat_invalid_time")
			continue
		}
		message := XChatMessage{SequenceID: evt.SequenceID, MessageID: evt.MessageID, SenderID: evt.SenderID, ConversationID: evt.ConversationID, PreviousSequenceID: evt.PreviousSequenceID, CreatedAt: evt.CreatedAt, Kind: evt.Kind, Text: evt.Text, Encrypted: evt.Encrypted, SignaturePresent: evt.SignaturePresent, Attachments: evt.Attachments}
		if evt.Kind == "human" {
			if evt.MessageID != "" && messageIDs[evt.MessageID] {
				return XChatMessagePage{}, dmReadError("xchat_duplicate_message_identity")
			}
			messageIDs[evt.MessageID] = true
			message.RecipientID = conv.PeerID
			if evt.SenderID == conv.PeerID {
				message.RecipientID = conv.SenderID
			}
			if evt.MessageID == "" {
				gap("xchat_missing_message_identity")
			}
			if evt.Encrypted {
				message.Text = ""
				gap("xchat_encrypted_content")
			}
			if evt.Attachments {
				gap("unsupported_attachment")
			}
		} else {
			page.Coverage.UnknownEventEntries++
			gap("unsupported_event")
		}
		page.Events = append(page.Events, message)
	}
	if page.HasMore {
		page.Coverage.CursorState = "present"
		if page.NextCursor == "" || !chatSequenceLess(page.NextCursor, cursor) {
			gap("xchat_cursor_stalled")
			page.Coverage.PaginationSupported = false
		}
	} else {
		page.Coverage.CursorState = "exhausted"
	}
	return page, nil
}
func chatSequence(s string) bool {
	if !dmNumericID(s) || s[0] == '0' {
		return false
	}
	n, err := strconv.ParseUint(s, 10, 63)
	return err == nil && n > 0
}
func chatSequenceLess(a, b string) bool {
	x, _ := strconv.ParseUint(a, 10, 63)
	y, _ := strconv.ParseUint(b, 10, 63)
	return x < y
}

// Native one-to-one XChat IDs join lexicographically sorted numeric members
// with ':'. Legacy REST uses '-' and is intentionally not accepted here.
// Protocol facts: mautrix/twitter 726c742bf21211649d3bca4382f5467d07fcd735,
// pkg/twittermeow/methods/methods.go CreateConversationID and
// pkg/connector/conversationdata.go NormalizeConversationID. No code copied.
func chatConversationID(id, sender, peer string) bool {
	parts := strings.Split(id, ":")
	return len(parts) == 2 && dmNumericID(parts[0]) && dmNumericID(parts[1]) && parts[0][0] != '0' && parts[1][0] != '0' && parts[0] < parts[1] && (parts[0] == sender && parts[1] == peer || parts[0] == peer && parts[1] == sender)
}

// XChatMembershipDiagnostics contains only bounded comparisons and counts from
// a rejected decoded membership response. It never authenticates membership.
type XChatMembershipDiagnostics struct {
	ConversationIDPresent                   bool
	ConversationIDMatchesRequested          bool
	ConversationIDMatchesLegacyPair         bool
	ConversationIDMatchesReversedRequested  bool
	ConversationIDMatchesReversedLegacyPair bool
	ParticipantCount                        int
	ParticipantsTruncated                   bool
	ExpectedSenderMatches                   int
	ExpectedPeerMatches                     int
	ParticipantConflictCount                int
	MissingNumericCount                     int
	GroupState                              string
}

type chatMembershipParticipant struct {
	ID     string `json:"rest_id"`
	Result *struct {
		ID string `json:"rest_id"`
	} `json:"result"`
}

func chatMembershipDiagnostics(id string, group json.RawMessage, participants []chatMembershipParticipant, requested, sender, peer string) *XChatMembershipDiagnostics {
	parts := strings.Split(requested, ":")
	reversed := ""
	if len(parts) == 2 {
		reversed = parts[1] + ":" + parts[0]
	}
	d := &XChatMembershipDiagnostics{ConversationIDPresent: id != "", ConversationIDMatchesRequested: id == requested, ConversationIDMatchesLegacyPair: id == strings.ReplaceAll(requested, ":", "-"), ConversationIDMatchesReversedRequested: reversed != "" && id == reversed, ConversationIDMatchesReversedLegacyPair: reversed != "" && id == strings.ReplaceAll(reversed, ":", "-"), ParticipantCount: min(len(participants), 3), ParticipantsTruncated: len(participants) > 3, GroupState: "other"}
	raw := strings.TrimSpace(string(group))
	switch {
	case len(group) == 0:
		d.GroupState = "absent"
	case raw == "null":
		d.GroupState = "null"
	default:
		if len(raw) >= 2 && raw[0] == '{' && raw[len(raw)-1] == '}' && json.Valid(group) {
			d.GroupState = "object"
			if strings.TrimSpace(raw[1:len(raw)-1]) == "" {
				d.GroupState = "empty_object"
			}
		}
	}
	for _, p := range participants[:min(len(participants), 3)] {
		resolved := p.ID
		if p.Result != nil {
			if p.ID != "" && p.Result.ID != "" && p.ID != p.Result.ID {
				d.ParticipantConflictCount++
			}
			if resolved == "" {
				resolved = p.Result.ID
			}
		}
		if !dmNumericID(resolved) {
			d.MissingNumericCount++
		}
		if resolved == sender {
			d.ExpectedSenderMatches++
		}
		if resolved == peer {
			d.ExpectedPeerMatches++
		}
	}
	return d
}
