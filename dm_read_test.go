package x

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestDMReadRejectsUnsupportedAndMalformed(t *testing.T) {
	for _, body := range []string{`{}`, `{"conversation_timeline":null}`, `{"conversation_timeline":{"entries":null}}`, `{"conversation_timeline":{"entries":[{"message":{"message_data":{"id":"1"}}}]}}`, `{"conversation_timeline":{"entries":[{"message":{"message_data":{"id":"1","sender_id":"2","conversation_id":"2-3","time":"bad"}}}]}}`} {
		if _, err := parseMessages(json.RawMessage(body)); !errors.Is(err, ErrRequestFailed) {
			t.Fatalf("expected classified failure, got %v", err)
		}
	}
	if _, err := parseConversations(json.RawMessage(`{"inbox_initial_state":{}}`)); !errors.Is(err, ErrRequestFailed) {
		t.Fatal(err)
	}
}

func TestDMReadMessagesAndCoverage(t *testing.T) {
	body := json.RawMessage(`{"conversation_timeline":{"entries":[{"message":{"id":"9","conversation_id":"2-3","time":"1700000000123","message_data":{"id":"9","sender_id":"2","recipient_id":"3","conversation_id":"2-3","time":"1700000000123","text":"hello"}}},{"message":{"id":"10","conversation_id":"2-3","time":"1700000000124","message_data":{"id":"10","sender_id":"3","recipient_id":"2","conversation_id":"2-3","time":"1700000000124","attachment":{"photo":{}}}}},{"conversation_create":{"id":"8"}}],"min_entry_id":"8","status":"HAS_MORE"}}`)
	page, err := parseMessages(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 2 || page.Messages[0].RecipientID != "3" || page.Messages[0].CreatedAt.UnixMilli() != 1700000000123 || page.Messages[0].Kind != "human" {
		t.Fatalf("bad message identity/time: %+v", page)
	}
	if !page.HasNext || page.NextCursor != "8" || page.Coverage.State != "partial" || page.Coverage.UnsupportedEntries != 1 || len(page.Messages[1].Attachments) != 1 {
		t.Fatalf("bad coverage: %+v", page)
	}
	// Importers must deduplicate overlapping pages by provider message ID.
	again, err := parseMessages(body)
	if err != nil || again.Messages[0].ID != page.Messages[0].ID {
		t.Fatal("unstable overlap identity")
	}
}

func TestDMCursorOnlyAndUnsupportedContinuation(t *testing.T) {
	page, err := parseMessages(json.RawMessage(`{"conversation_timeline":{"entries":[],"min_entry_id":"8","status":"HAS_MORE"}}`))
	if err != nil || !page.HasNext || page.NextCursor != "8" {
		t.Fatalf("cursor-only page lost: %+v %v", page, err)
	}
	inbox, err := parseConversations(json.RawMessage(`{"inbox_initial_state":{"conversations":{},"cursor":"next"}}`))
	if err != nil || !inbox.HasNext || inbox.Coverage.PaginationSupported {
		t.Fatalf("bad inbox coverage %+v %v", inbox, err)
	}
	// A nil client proves unverified continuation returns before network access.
	var client *Client
	if _, err = client.GetConversationsPage(context.Background(), DMPageOptions{Cursor: "next"}); !errors.Is(err, ErrInvalidParams) {
		t.Fatal(err)
	}
	if _, err = client.GetConversationPage(context.Background(), "2-3", DMPageOptions{Cursor: "not-numeric"}); !errors.Is(err, ErrInvalidParams) {
		t.Fatal(err)
	}
}

// Synthetic fixture follows key presence observed in the consented 2026-10-17
// inbox read; it contains no copied private provider values or message bodies.
func TestDMReadObservedOuterEnvelope(t *testing.T) {
	body := json.RawMessage(`{"conversation_timeline":{"entries":[{"message":{"id":"9","conversation_id":"2-3","time":"1700000000999","message_data":{"id":"9","sender_id":"2","recipient_id":"3","time":"1700000000123","text":"fixture"}}}],"status":"AT_END"}}`)
	page, err := parseMessages(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ConversationID != "2-3" || page.Messages[0].ID != "9" {
		t.Fatalf("lost canonical identity %+v", page)
	}
	var v map[string]any
	_ = json.Unmarshal(body, &v)
	entry := v["conversation_timeline"].(map[string]any)["entries"].([]any)[0].(map[string]any)["message"].(map[string]any)["message_data"].(map[string]any)
	entry["conversation_id"] = "4-5"
	bad, _ := json.Marshal(v)
	if _, err := parseMessages(bad); !errors.Is(err, ErrRequestFailed) {
		t.Fatal("conflicting conversation accepted")
	}
}

func TestDMReadInboxMembership(t *testing.T) {
	page, err := parseConversations(json.RawMessage(`{"inbox_initial_state":{"conversations":{"2-3":{"conversation_id":"2-3","type":"ONE_TO_ONE","participants":[{"user_id":"2"},{"user_id":"3"}]}}}}`))
	if err != nil || len(page.Conversations) != 1 || len(page.Conversations[0].Participants) != 2 {
		t.Fatalf("membership lost %+v %v", page, err)
	}
	if _, err := parseConversations(json.RawMessage(`{"inbox_initial_state":{"conversations":{"2-3":{"conversation_id":"4-5"}}}}`)); !errors.Is(err, ErrRequestFailed) {
		t.Fatal("map identity conflict accepted")
	}
}

func TestDMReadSeparatesUpdateAndHistoryCursors(t *testing.T) {
	page, err := parseConversations(json.RawMessage(`{"inbox_initial_state":{"conversations":{},"cursor":"opaque-update","inbox_timelines":{"trusted":{"status":"HAS_MORE","min_entry_id":"123"},"untrusted":{"status":"AT_END"}}}}`))
	if err != nil || page.UpdateCursor != "opaque-update" || page.InboxHistory["trusted"].Cursor != "123" || page.InboxHistory["untrusted"].Status != "AT_END" {
		t.Fatalf("cursor contracts mixed %+v %v", page, err)
	}
	var c *Client
	for _, opts := range []DMInboxPageOptions{{Cursor: "123"}, {Folder: "trusted"}, {Folder: "other", Cursor: "123"}, {Folder: "trusted", Cursor: "opaque-update"}} {
		if _, err := c.GetInboxPage(context.Background(), opts); !errors.Is(err, ErrInvalidParams) {
			t.Fatalf("invalid options reached provider %+v %v", opts, err)
		}
	}
}

func TestDMReadHistoryContinuationRequest(t *testing.T) {
	calls := 0
	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/i/api/1.1/dm/conversation/2-3.json" || r.URL.Query().Get("max_id") != "8" || r.URL.Query().Get("context") != "FETCH_DM_CONVERSATION_HISTORY" || r.URL.Query().Get("include_conversation_info") != "true" {
			t.Fatalf("incorrect history request")
		}
		return jsonResponse(200, `{"conversation_timeline":{"entries":[],"status":"AT_END"}}`), nil
	}))
	page, err := c.GetConversationPage(context.Background(), "2-3", DMPageOptions{Cursor: "8"})
	if err != nil || calls != 1 || page.HasNext {
		t.Fatalf("continuation calls=%d page=%+v err=%v", calls, page, err)
	}
}

func TestDMReadInboxMessagesCarryObservedHeads(t *testing.T) {
	page, err := parseConversations(json.RawMessage(`{"inbox_initial_state":{"conversations":{"2-3":{"conversation_id":"2-3","type":"ONE_TO_ONE","last_read_event_id":"90","participants":[{"user_id":"2"},{"user_id":"3"}]}},"entries":[{"message":{"id":"9","conversation_id":"2-3","time":"1700000000999","message_data":{"id":"9","sender_id":"2","recipient_id":"3","time":"1700000000123","text":"fixture"}}},{"conversation_read":{"id":"90"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].CreatedAt.UnixMilli() != 1700000000123 || page.Messages[0].EventAt.UnixMilli() != 1700000000999 {
		t.Fatalf("event/message times mixed %+v", page)
	}
	conv := page.Conversations[0]
	if conv.LastMessage == nil || conv.LastMessage.ID != "9" || conv.LatestEventID != "9" || conv.LatestEventAt.UnixMilli() != 1700000000999 || conv.Coverage.State != "partial" || conv.Coverage.UnsupportedEntries != 1 {
		t.Fatalf("head metadata missing %+v", conv)
	}
}

func TestConversationReadNoticeClassification(t *testing.T) {
	for _, test := range []struct {
		name, entry string
		known       bool
	}{
		{"read", `{"conversation_read":{"id":"900","conversation_id":"123-456","time":"1700000000000","last_read_event_id":"899"}}`, true},
		{"missing_identity", `{"conversation_read":{"id":"900","time":"1700000000000","last_read_event_id":"899"}}`, false},
		{"bad_time", `{"conversation_read":{"id":"900","conversation_id":"123-456","time":"bad","last_read_event_id":"899"}}`, false},
		{"unknown", `{"conversation_create":{"id":"900"}}`, false},
		{"mixed", `{"conversation_read":{"id":"900","conversation_id":"123-456","time":"1700000000000","last_read_event_id":"899"},"unknown":{}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			page, err := parseMessages([]byte(`{"conversation_timeline":{"entries":[` + test.entry + `],"status":"AT_END"}}`))
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Messages) != 0 {
				t.Fatal("notice became human activity")
			}
			if test.known {
				if page.Coverage.KnownNoticeEntries != 1 || page.Coverage.UnknownEventEntries != 0 || page.Coverage.UnsupportedEntries != 0 {
					t.Fatal(page.Coverage)
				}
			} else if page.Coverage.KnownNoticeEntries != 0 || page.Coverage.UnknownEventEntries != 1 || page.Coverage.UnsupportedEntries != 1 {
				t.Fatal(page.Coverage)
			}
		})
	}
}

func TestDMReadMixedMessageEnvelopeRetainsUnknownGap(t *testing.T) {
	page, err := parseMessages([]byte(`{"conversation_timeline":{"entries":[{"message":{"id":"9","conversation_id":"2-3","time":"1700000000123","message_data":{"id":"9","sender_id":"2","recipient_id":"3","time":"1700000000123","text":"fixture"}},"unknown":{}}],"status":"AT_END"}}`))
	if err != nil || len(page.Messages) != 1 || page.Coverage.UnknownEventEntries != 1 || page.Coverage.UnsupportedEntries != 1 {
		t.Fatal(page.Coverage, err)
	}
}
