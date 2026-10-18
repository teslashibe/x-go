package x

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// knownCallEnd recognizes only a validated non-human audio/video call notice.
// Primary schema and closed enums:
// https://github.com/TextsHQ/platform-twitter/blob/aea633e2b8e1fac9ff1dd1ebe90550a5719c9174/src/twitter-types.ts#L29-L42
// Call notice semantics (missed/canceled/declined/ended, not message text):
// https://github.com/TextsHQ/platform-twitter/blob/aea633e2b8e1fac9ff1dd1ebe90550a5719c9174/src/mappers.ts#L285-L301
// Independent conversation-bound Call dispatch:
// https://github.com/mahrtayyab/tweety/blob/f6bfc8610bd3ec8af2fe000076bc1b53a6327e7d/src/tweety/types/inbox.py#L293-L294
// The returned identity participates in the same requested-history binding as
// read notices. Recognition never adds a human Message or proves completeness.
func knownCallEnd(entry map[string]json.RawMessage) string {
	if len(entry) != 1 {
		return ""
	}
	raw, ok := entry["end_av_broadcast"]
	if !ok {
		return ""
	}
	var notice struct {
		ID             string `json:"id"`
		ConversationID string `json:"conversation_id"`
		Time           string `json:"time"`
		IsCaller       *bool  `json:"is_caller"`
		StartedAt      string `json:"started_at_ms"`
		EndedAt        string `json:"ended_at_ms"`
		EndReason      string `json:"end_reason"`
		CallType       string `json:"call_type"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&notice) != nil || !dmNumericID(notice.ID) || !dmConversationID(notice.ConversationID) || notice.IsCaller == nil {
		return ""
	}
	for _, stamp := range []string{notice.Time, notice.StartedAt, notice.EndedAt} {
		millis, err := strconv.ParseInt(stamp, 10, 64)
		if err != nil || millis <= 0 {
			return ""
		}
	}
	start, _ := strconv.ParseInt(notice.StartedAt, 10, 64)
	end, _ := strconv.ParseInt(notice.EndedAt, 10, 64)
	if start > end {
		return ""
	}
	switch notice.CallType {
	case "AUDIO_ONLY", "VIDEO":
	default:
		return ""
	}
	switch notice.EndReason {
	case "MISSED", "CANCELED", "DECLINED", "HUNG_UP", "TIMED_OUT":
	default:
		return ""
	}
	return notice.ConversationID
}
