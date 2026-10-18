package x

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// Synthetic schema-only fixtures, never captured provider content. Primary
// source URLs and pinned commits are documented in dm_call_notice.go.
func callEndFixture() map[string]any {
	return map[string]any{"id": "900", "conversation_id": "2-3", "time": "1700000003000", "is_caller": false, "started_at_ms": "1700000001000", "ended_at_ms": "1700000002000", "end_reason": "MISSED", "call_type": "AUDIO_ONLY"}
}
func callNoticePage(t *testing.T, entries ...any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"conversation_timeline": map[string]any{"entries": entries, "status": "AT_END"}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestDMCallEndNoticeIsNotHumanActivity(t *testing.T) {
	for _, typ := range []string{"AUDIO_ONLY", "VIDEO"} {
		for _, reason := range []string{"MISSED", "CANCELED", "DECLINED", "HUNG_UP", "TIMED_OUT"} {
			n := callEndFixture()
			n["call_type"] = typ
			n["end_reason"] = reason
			page, err := parseMessages(callNoticePage(t, map[string]any{"end_av_broadcast": n}))
			if err != nil || len(page.Messages) != 0 || page.Coverage.KnownNoticeEntries != 1 || page.Coverage.UnknownEventEntries != 0 || page.Coverage.UnsupportedEntries != 0 || page.Coverage.State != "partial" {
				t.Fatal(page, err)
			}
		}
	}
	message := map[string]any{"id": "901", "conversation_id": "2-3", "time": "1700000004000", "message_data": map[string]any{"id": "901", "sender_id": "2", "recipient_id": "3", "time": "1700000004000", "text": "synthetic human"}}
	page, err := parseMessages(callNoticePage(t, map[string]any{"end_av_broadcast": callEndFixture()}, map[string]any{"message": message}))
	if err != nil || len(page.Messages) != 1 || page.Messages[0].ID != "901" || page.Messages[0].Kind != "human" || page.Coverage.KnownNoticeEntries != 1 || page.Coverage.State != "partial" {
		t.Fatal(page, err)
	}
}
func TestDMCallEndMalformedOrUnknownRetainsGap(t *testing.T) {
	tests := []struct {
		name, key string
		value     any
	}{
		{"id", "id", "bad"}, {"conversation", "conversation_id", "bad"}, {"time", "time", "bad"}, {"zero_time", "time", "0"}, {"start", "started_at_ms", "-1"}, {"end_before_start", "ended_at_ms", "1"}, {"overflow", "time", "999999999999999999999"}, {"caller_type", "is_caller", "false"}, {"caller_missing", "is_caller", nil}, {"call_type", "call_type", "UNKNOWN"}, {"reason", "end_reason", "UNKNOWN"}, {"extra_field", "unrecognized", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := callEndFixture()
			if tc.value == nil {
				delete(n, tc.key)
			} else {
				n[tc.key] = tc.value
			}
			page, err := parseMessages(callNoticePage(t, map[string]any{"end_av_broadcast": n}))
			if err != nil || len(page.Messages) != 0 || page.Coverage.KnownNoticeEntries != 0 || page.Coverage.UnknownEventEntries != 1 || page.Coverage.UnsupportedEntries != 1 || page.Coverage.State != "partial" {
				t.Fatal(page, err)
			}
		})
	}
	for _, entry := range []any{map[string]any{"unknown_call": callEndFixture()}, map[string]any{"end_av_broadcast": callEndFixture(), "unknown": map[string]any{}}} {
		page, err := parseMessages(callNoticePage(t, entry))
		if err != nil || page.Coverage.UnknownEventEntries != 1 || page.Coverage.KnownNoticeEntries != 0 {
			t.Fatal(page, err)
		}
	}
}
func TestDMCallEndNoticeMustMatchRequestedConversation(t *testing.T) {
	for _, cursor := range []string{"", "8"} {
		for _, cid := range []string{"2-3", "4-5"} {
			n := callEndFixture()
			n["conversation_id"] = cid
			client := newTestClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(200, string(callNoticePage(t, map[string]any{"end_av_broadcast": n}))), nil
			}))
			_, err := client.GetConversationPage(context.Background(), "2-3", DMPageOptions{Cursor: cursor})
			if cid == "2-3" && err != nil || cid != "2-3" && !errors.Is(err, ErrRequestFailed) {
				t.Fatal(cursor, cid, err)
			}
		}
	}
}
