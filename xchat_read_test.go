package x

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

type chatReadTransport func(*http.Request) (*http.Response, error)

func (f chatReadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func chatReadClient(f chatReadTransport) *Client {
	return &Client{httpClient: &http.Client{Transport: f}, cookies: Cookies{AuthToken: "fixture-auth", CT0: "fixture-csrf"}, restID: "123", userAgent: "fixture"}
}
func chatReadResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

const chatMembershipFixture = `{"data":{"get_inbox_page_conversation_data":{"data":{"conversation_detail":{"conversation_id":"123:456","participants_results":[{"rest_id":"123","result":{"rest_id":"123"}},{"result":{"rest_id":"456"}}]}}}}}`

func chatReadSeal(c *Client) XChatConversation {
	return XChatConversation{ID: "123:456", SenderID: "123", PeerID: "456", ParticipantIDs: []string{"123", "456"}, client: c, conversation: "123:456", sender: "123", peer: "456"}
}

// Independent synthetic TBinary event construction from official MIT schema.
func chatWireField(kind byte, id int16, value []byte) []byte {
	return append([]byte{kind, byte(id >> 8), byte(id)}, value...)
}
func chatWireString(s string) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(len(s)))
	return append(b, s...)
}
func chatWireStruct(fields ...[]byte) []byte {
	var b []byte
	for _, f := range fields {
		b = append(b, f...)
	}
	return append(b, 0)
}
func chatWireEvent(sequence, sender, conv string, detail []byte, messageIDs ...string) string {
	messageID := fmt.Sprintf("a9ad4597-3869-4c76-9d36-%012s", sequence)
	if len(messageIDs) > 0 {
		messageID = messageIDs[0]
	}
	raw := chatWireStruct(chatWireField(11, 1, chatWireString(sequence)), chatWireField(11, 2, chatWireString(messageID)), chatWireField(11, 3, chatWireString(sender)), chatWireField(11, 4, chatWireString(conv)), chatWireField(11, 6, chatWireString("1792308000000")), chatWireField(12, 7, detail))
	return base64.StdEncoding.EncodeToString(raw)
}
func chatWirePlain(text string) []byte {
	content := chatWireStruct(chatWireField(12, 1, chatWireStruct(chatWireField(12, 1, chatWireStruct(chatWireField(11, 1, chatWireString(text)))))))
	return chatWireStruct(chatWireField(12, 1, chatWireStruct(chatWireField(11, 100, chatWireString(string(content))))))
}
func chatPageFixture(events []string, more bool, keys []string) string {
	v := map[string]any{"data": map[string]any{"get_conversation_page": map[string]any{"encoded_message_events": events, "has_more": more, "missing_conversation_key_change_events": keys}}}
	b, _ := json.Marshal(v)
	return string(b)
}

func TestXChatMembershipExactRequestAndSeal(t *testing.T) {
	calls := 0
	c := chatReadClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Scheme != "https" || r.URL.Host != "api.x.com" || r.URL.Path != "/graphql/"+chatMembershipPath || r.Header.Get("X-Csrf-Token") != "fixture-csrf" || !strings.Contains(r.Header.Get("Cookie"), "auth_token=fixture-auth") {
			t.Fatal("wrong native request")
		}
		var vars map[string]any
		if json.Unmarshal([]byte(r.URL.Query().Get("variables")), &vars) != nil || len(vars) != 2 || vars["conversation_id"] != "123:456" || vars["include_user_public_keys"] != false {
			t.Fatal("wrong variables")
		}
		return chatReadResponse(chatMembershipFixture), nil
	})
	conv, err := c.GetXChatConversation(context.Background(), "123:456", "123", "456")
	if err != nil || len(conv.ParticipantIDs) != 2 || calls != 1 {
		t.Fatal(conv, err, calls)
	}
	conv.ID = "123:789"
	if _, err = c.GetXChatConversationPage(context.Background(), conv, XChatPageOptions{}); !errors.Is(err, ErrInvalidParams) || calls != 1 {
		t.Fatal("mutated membership dispatched")
	}
	other := chatReadClient(nil)
	conv = chatReadSeal(c)
	if _, err = other.GetXChatConversationPage(context.Background(), conv, XChatPageOptions{}); !errors.Is(err, ErrInvalidParams) {
		t.Fatal("transferred membership")
	}
}

func TestXChatMembershipRejectsMismatches(t *testing.T) {
	cases := []string{strings.Replace(chatMembershipFixture, `"conversation_id":"123:456"`, `"conversation_id":"123:789"`, 1), strings.Replace(chatMembershipFixture, `"rest_id":"456"`, `"rest_id":"789"`, 1), strings.Replace(chatMembershipFixture, `"result":{"rest_id":"456"}`, `"rest_id":"123"`, 1), strings.Replace(chatMembershipFixture, `"participants_results":`, `"group_metadata":{},"participants_results":`, 1), `{"data":{"get_inbox_page_conversation_data":null}}`}
	for _, body := range cases {
		calls := 0
		c := chatReadClient(func(*http.Request) (*http.Response, error) { calls++; return chatReadResponse(body), nil })
		if _, err := c.GetXChatConversation(context.Background(), "123:456", "123", "456"); err == nil || calls != 1 {
			t.Fatal("accepted mismatched membership")
		}
	}
}

func TestXChatHistoryRequestPlaintextAndPagination(t *testing.T) {
	calls := 0
	events := []string{chatWireEvent("200", "456", "123:456", chatWirePlain("synthetic text")), chatWireEvent("190", "123", "123:456", chatWireStruct(chatWireField(12, 6, chatWireStruct())))}
	c := chatReadClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Host != "api.x.com" || r.URL.Path != "/graphql/"+chatHistoryPath {
			t.Fatal("wrong history endpoint")
		}
		var vars map[string]any
		_ = json.Unmarshal([]byte(r.URL.Query().Get("variables")), &vars)
		if len(vars) != 4 || vars["conversation_id"] != "123:456" || vars["min_local_sequence_id"] != chatInitialSequence || vars["min_conversation_key_version"] != chatInitialSequence {
			t.Fatal("wrong history variables")
		}
		settings := vars["query_settings"].(map[string]any)
		if len(settings) != 4 || settings["conversation_event_limit"] != float64(200) {
			t.Fatal("wrong settings")
		}
		return chatReadResponse(chatPageFixture(events, true, []string{"not-decoded-not-cursor"})), nil
	})
	page, err := c.GetXChatConversationPage(context.Background(), chatReadSeal(c), XChatPageOptions{})
	if err != nil || calls != 1 || len(page.Events) != 2 || page.Events[0].Text != "synthetic text" || page.Events[0].RecipientID != "123" || page.Events[0].SequenceID == page.Events[0].MessageID || page.Events[0].Verified || page.NextCursor != "190" || !page.HasMore || page.SupplementalKeyEvents != 1 || page.Coverage.State != "partial" {
		t.Fatal(page, err, calls)
	}
}

func TestXChatHistoryEncryptedUnknownAndStalled(t *testing.T) {
	encrypted := chatWireStruct(chatWireField(12, 1, chatWireStruct(chatWireField(11, 100, chatWireString("ciphertext")), chatWireField(11, 101, chatWireString("key-version")))))
	for _, tc := range []struct {
		events         []string
		more           bool
		cursor, reason string
	}{{[]string{chatWireEvent("200", "456", "123:456", encrypted)}, false, "", "xchat_encrypted_content"}, {[]string{"not-base64"}, true, "", "xchat_cursor_stalled"}} {
		c := chatReadClient(func(*http.Request) (*http.Response, error) {
			return chatReadResponse(chatPageFixture(tc.events, tc.more, nil)), nil
		})
		page, err := c.GetXChatConversationPage(context.Background(), chatReadSeal(c), XChatPageOptions{Cursor: tc.cursor})
		if err != nil || page.Coverage.State != "partial" || page.Coverage.Reason != tc.reason {
			t.Fatal(page, err)
		}
		for _, evt := range page.Events {
			if evt.Encrypted && evt.Text != "" || evt.Verified {
				t.Fatal("unverified content upgraded")
			}
		}
	}
}

func TestXChatHistoryIdentityAndMalformedPages(t *testing.T) {
	bodies := []string{chatPageFixture([]string{chatWireEvent("200", "789", "123:456", chatWirePlain("wrong sender"))}, false, nil), chatPageFixture([]string{chatWireEvent("200", "456", "123:789", chatWirePlain("wrong conversation"))}, false, nil), `{"data":{"get_conversation_page":{"encoded_message_events":[],"has_more":null}}}`, `{"data":{"get_conversation_page":{"has_more":false}}}`, `{"data":{"get_conversation_page":null}}`, strings.Repeat("x", maxResponseBody+1), `{"data":{"get_conversation_page":{"encoded_message_events":[],"has_more":false}},"errors"
 "fmt":[{"message":"partial","kind":"NonFatal"}]}`}
	for _, body := range bodies {
		calls := 0
		c := chatReadClient(func(*http.Request) (*http.Response, error) { calls++; return chatReadResponse(body), nil })
		if _, err := c.GetXChatConversationPage(context.Background(), chatReadSeal(c), XChatPageOptions{}); err == nil || calls != 1 {
			t.Fatal("accepted invalid history", calls)
		}
	}
}

func TestXChatRequestErrorsCancellationAndNoRetry(t *testing.T) {
	sentinel := errors.New("fixture transport")
	c := chatReadClient(func(*http.Request) (*http.Response, error) { return nil, sentinel })
	c.maxRetries = 3
	if _, err := c.GetXChatConversation(context.Background(), "123:456", "123", "456"); !errors.Is(err, sentinel) {
		t.Fatal("transport cause lost", err)
	}
	for _, status := range []int{401, 403, 429, 500} {
		calls := 0
		c := chatReadClient(func(*http.Request) (*http.Response, error) {
			calls++
			r := chatReadResponse(`{}`)
			r.StatusCode = status
			return r, nil
		})
		if _, err := c.GetXChatConversation(context.Background(), "123:456", "123", "456"); err == nil || calls != 1 {
			t.Fatal("retry/error contract", calls)
		}
	}
	calls := 0
	c = chatReadClient(func(*http.Request) (*http.Response, error) { calls++; return nil, sentinel })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GetXChatConversation(ctx, "123:456", "123", "456"); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("cancelled request dispatched")
	}
	for _, cursor := range []string{"0", "-1", "not-sequence", "9223372036854775808"} {
		if _, err := c.GetXChatConversationPage(context.Background(), chatReadSeal(c), XChatPageOptions{Cursor: cursor}); !errors.Is(err, ErrInvalidParams) || calls != 0 {
			t.Fatal("invalid cursor dispatched")
		}
	}
}

func TestXChatRedirectRefusedWithoutSecondRequest(t *testing.T) {
	calls := 0
	c := chatReadClient(func(*http.Request) (*http.Response, error) {
		calls++
		r := chatReadResponse(`{}`)
		r.StatusCode = 302
		r.Header.Set("Location", "https://api.x.com/another")
		return r, nil
	})
	if _, err := c.GetXChatConversation(context.Background(), "123:456", "123", "456"); err == nil || calls != 1 {
		t.Fatal("redirect issued extra request", calls, err)
	}
}

func TestXChatHistoryNativeIdentityAndTerminalBounds(t *testing.T) {
	duplicate := "a9ad4597-3869-4c76-9d36-7395ac41aa5c"
	cases := []struct {
		events []string
		cursor string
		more   bool
	}{
		{[]string{chatWireEvent("200", "123", "123:456", chatWirePlain("first"), duplicate), chatWireEvent("190", "456", "123:456", chatWirePlain("conflict"), duplicate)}, "", false},
		{[]string{chatWireEvent("200", "456", "123:456", chatWirePlain("repeat"))}, "200", false},
		{[]string{chatWireEvent("201", "456", "123:456", chatWirePlain("newer")), chatWireEvent("190", "456", "123:456", chatWirePlain("older"))}, "200", false},
		{[]string{chatWireEvent("201", "456", "123:456", chatWirePlain("newer")), chatWireEvent("190", "456", "123:456", chatWirePlain("older"))}, "200", true},
	}
	for _, tc := range cases {
		c := chatReadClient(func(*http.Request) (*http.Response, error) {
			return chatReadResponse(chatPageFixture(tc.events, tc.more, nil)), nil
		})
		if _, err := c.GetXChatConversationPage(context.Background(), chatReadSeal(c), XChatPageOptions{Cursor: tc.cursor}); err == nil {
			t.Fatal("accepted contradictory identity/cursor")
		}
	}
	c := chatReadClient(func(*http.Request) (*http.Response, error) {
		return chatReadResponse(chatPageFixture([]string{chatWireEvent("190", "456", "123:456", chatWirePlain("older"))}, false, nil)), nil
	})
	page, err := c.GetXChatConversationPage(context.Background(), chatReadSeal(c), XChatPageOptions{Cursor: "200"})
	if err != nil || page.Coverage.State != "supported" || page.HasMore || page.NextCursor != "190" {
		t.Fatal(page, err)
	}
}
func TestXChatMembershipParticipantAndSessionMutation(t *testing.T) {
	calls := 0
	c := chatReadClient(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected HTTP") })
	conv := chatReadSeal(c)
	conv.ParticipantIDs[1] = "789"
	if _, err := c.GetXChatConversationPage(context.Background(), conv, XChatPageOptions{}); !errors.Is(err, ErrInvalidParams) || calls != 0 {
		t.Fatal("participant mutation admitted")
	}
	conv = chatReadSeal(c)
	c.restID = "789"
	if _, err := c.GetXChatConversationPage(context.Background(), conv, XChatPageOptions{}); !errors.Is(err, ErrInvalidParams) || calls != 0 {
		t.Fatal("session identity mutation admitted")
	}
}
func TestDMMessageIdentityOptionalJSON(t *testing.T) {
	old, err := json.Marshal(Message{})
	if err != nil || strings.Contains(string(old), `"identity"`) {
		t.Fatal("legacy JSON changed")
	}
	message := Message{Identity: &DMMessageIdentity{Protocol: "xchat", NativeMessageID: "a9ad4597-3869-4c76-9d36-7395ac41aa5c", SequenceID: "200", VerificationMode: "server_authenticated", RecipientSource: "authenticated_membership"}}
	raw, err := json.Marshal(message)
	var parsed Message
	if err != nil || json.Unmarshal(raw, &parsed) != nil || parsed.Identity == nil || parsed.Identity.CryptographicVerified || parsed.Identity.SequenceID != "200" || parsed.Identity.NativeMessageID != message.Identity.NativeMessageID {
		t.Fatal("identity contract changed")
	}
}

func TestXChatNativeConversationFormatAndUnequalLengthMembers(t *testing.T) {
	calls := 0
	c := chatReadClient(nil)
	c.restID = "100"
	c.httpClient.Transport = chatReadTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var vars map[string]any
		_ = json.Unmarshal([]byte(r.URL.Query().Get("variables")), &vars)
		if vars["conversation_id"] != "100:9" {
			t.Fatal("native ID rewritten")
		}
		body := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(chatMembershipFixture, "123:456", "100:9"), `"123"`, `"100"`), `"456"`, `"9"`)
		return chatReadResponse(body), nil
	})
	conv, err := c.GetXChatConversation(context.Background(), "100:9", "100", "9")
	if err != nil || conv.ID != "100:9" || calls != 1 {
		t.Fatal(conv, err, calls)
	}
	for _, bad := range []string{"100-9", "9:100", "100:8", "100:100", "0100:9", "100:9:8"} {
		if _, err := c.GetXChatConversation(context.Background(), bad, "100", "9"); !errors.Is(err, ErrInvalidParams) || calls != 1 {
			t.Fatal("invalid/foreign/alias conversation dispatched", bad, err)
		}
	}
	for _, bad := range []string{"00", "0400", "0001"} {
		if chatSequence(bad) {
			t.Fatal("sequence alias accepted", bad)
		}
	}
	if !dmConversationID("100-9") || dmConversationID("100:9") {
		t.Fatal("legacy validator changed")
	}
}

func TestXChatClientUUIDIsStablePrivateAndModernOnly(t *testing.T) {
	valid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	var ids []string
	c := chatReadClient(func(r *http.Request) (*http.Response, error) {
		id := r.Header.Get("X-Client-UUID")
		if !valid.MatchString(id) {
			t.Fatal("invalid client UUID")
		}
		ids = append(ids, id)
		if strings.HasSuffix(r.URL.Path, "GetInboxPageConversationDataRequestQuery") {
			return chatReadResponse(chatMembershipFixture), nil
		}
		return chatReadResponse(chatPageFixture([]string{}, false, nil)), nil
	})
	conv, err := c.GetXChatConversation(context.Background(), "123:456", "123", "456")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.GetXChatConversationPage(context.Background(), conv, XChatPageOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != ids[1] {
		t.Fatal("client UUID changed between membership and history")
	}
	legacy, _ := http.NewRequest(http.MethodGet, "https://x.com/i/api/1.1/dm/inbox_initial_state.json", nil)
	c.setHeaders(legacy)
	if legacy.Header.Get("X-Client-UUID") != "" {
		t.Fatal("modern header leaked to legacy headers")
	}
	other := chatReadClient(func(r *http.Request) (*http.Response, error) {
		if !valid.MatchString(r.Header.Get("X-Client-UUID")) || r.Header.Get("X-Client-UUID") == ids[0] {
			t.Fatal("client UUID reused")
		}
		return chatReadResponse(chatMembershipFixture), nil
	})
	if _, err = other.GetXChatConversation(context.Background(), "123:456", "123", "456"); err != nil {
		t.Fatal(err)
	}
}

func TestXChatMembershipTypedRejectionReasons(t *testing.T) {
	for _, tc := range []struct{ name, body, reason string }{
		{"cid", strings.Replace(chatMembershipFixture, `"conversation_id":"123:456"`, `"conversation_id":"123:789"`, 1), "xchat_membership_cid_mismatch"},
		{"count", `{"data":{"get_inbox_page_conversation_data":{"data":{"conversation_detail":{"conversation_id":"123:456","participants_results":[{"rest_id":"123"}]}}}}}`, "xchat_membership_participant_count"},
		{"empty group", `{"data":{"get_inbox_page_conversation_data":{"data":{"conversation_detail":{"conversation_id":"123:456","group_metadata":{},"participants_results":[]}}}}}`, "xchat_membership_group_present"},
		{"empty cid", `{"data":{"get_inbox_page_conversation_data":{"data":{"conversation_detail":{"conversation_id":"123:789","participants_results":[]}}}}}`, "xchat_membership_cid_mismatch"},
		{"group", strings.Replace(chatMembershipFixture, `"participants_results":`, `"group_metadata":{},"participants_results":`, 1), "xchat_membership_group_present"},
		{"conflict", strings.Replace(chatMembershipFixture, `"rest_id":"123","result":{"rest_id":"123"}`, `"rest_id":"123","result":{"rest_id":"789"}`, 1), "xchat_membership_participant_conflict"},
		{"pair", strings.ReplaceAll(chatMembershipFixture, `"456"`, `"789"`), "xchat_membership_pair_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := chatReadClient(func(*http.Request) (*http.Response, error) { calls++; return chatReadResponse(tc.body), nil })
			_, err := c.GetXChatConversation(context.Background(), "123:456", "123", "456")
			var typed *DMReadError
			if calls != 1 || !errors.As(err, &typed) || typed.Reason != tc.reason || typed.Membership == nil || !errors.Is(err, ErrRequestFailed) {
				t.Fatal("membership classification changed", calls, err)
			}
			if err.Error() != ErrRequestFailed.Error()+": DM response "+tc.reason {
				t.Fatal("error format changed")
			}
			if strings.Contains(err.Error(), "123") || strings.Contains(err.Error(), "789") {
				t.Fatal("identity leaked into error")
			}
		})
	}
}

func TestXChatMembershipDiagnosticsBoundedAndPrivate(t *testing.T) {
	var participants []chatMembershipParticipant
	if err := json.Unmarshal([]byte(`[{"rest_id":"123","result":{"rest_id":"789"}},{"result":{"rest_id":"456"}},{"rest_id":"PRIVATE_SENTINEL"},{"rest_id":"456"}]`), &participants); err != nil {
		t.Fatal(err)
	}
	d := chatMembershipDiagnostics("123-456", json.RawMessage(` { } `), participants, "123:456", "123", "456")
	if !d.ConversationIDPresent || d.ConversationIDMatchesRequested || !d.ConversationIDMatchesLegacyPair || d.ParticipantCount != 3 || !d.ParticipantsTruncated || d.ExpectedSenderMatches != 1 || d.ExpectedPeerMatches != 1 || d.ParticipantConflictCount != 1 || d.MissingNumericCount != 1 || d.GroupState != "empty_object" {
		t.Fatal("bounded diagnostics mismatch")
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"123", "456", "789", "PRIVATE_SENTINEL"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("provider value leaked")
		}
	}
	for _, tc := range []struct {
		id                string
		requested, legacy bool
	}{
		{"456:123", true, false}, {"456-123", false, true}, {"foreign", false, false},
	} {
		d := chatMembershipDiagnostics(tc.id, nil, nil, "123:456", "123", "456")
		if d.ConversationIDMatchesReversedRequested != tc.requested || d.ConversationIDMatchesReversedLegacyPair != tc.legacy {
			t.Fatal("reversed comparison mismatch")
		}
	}
	for _, tc := range []struct{ raw, state string }{{"", "absent"}, {"null", "null"}, {" null ", "null"}, {"{}", "empty_object"}, {`{"private":"PRIVATE_SENTINEL"}`, "object"}, {`[]`, "other"}, {`{invalid}`, "other"}, {`"PRIVATE_SENTINEL"`, "other"}} {
		if chatMembershipDiagnostics("", json.RawMessage(tc.raw), nil, "123:456", "123", "456").GroupState != tc.state {
			t.Fatal("group classification changed")
		}
	}
}

func TestXChatMembershipDiagnosticsOnlyDecodedRejections(t *testing.T) {
	for _, tc := range []struct {
		body   string
		facts  bool
		reason string
	}{
		{`{"data":{"get_inbox_page_conversation_data":null}}`, false, "xchat_missing_membership"},
		{strings.Replace(chatMembershipFixture, `"conversation_id":"123:456"`, `"conversation_id":"123-456"`, 1), true, "xchat_membership_cid_mismatch"},
		{strings.Replace(chatMembershipFixture, `"participants_results":`, `"group_metadata":{},"participants_results":`, 1), true, "xchat_membership_group_present"},
		{strings.Replace(chatMembershipFixture, `"result":{"rest_id":"456"}`, `"result":{"rest_id":""}`, 1), true, "xchat_missing_participant"},
	} {
		calls := 0
		c := chatReadClient(func(*http.Request) (*http.Response, error) { calls++; return chatReadResponse(tc.body), nil })
		_, err := c.GetXChatConversation(context.Background(), "123:456", "123", "456")
		var typed *DMReadError
		if calls != 1 || !errors.As(err, &typed) || typed.Reason != tc.reason || (typed.Membership != nil) != tc.facts || !errors.Is(err, ErrRequestFailed) {
			t.Fatal("decoded diagnostic scope changed", err)
		}
	}
}

func TestXChatMembershipAcceptsCanonicalOneToOneWithoutParticipants(t *testing.T) {
	for _, detail := range []string{`"conversation_id":"123:456","participants_results":[]`, `"conversation_id":"123:456"`, `"conversation_id":"123:456","group_metadata":null`} {
		body := `{"data":{"get_inbox_page_conversation_data":{"data":{"conversation_detail":{` + detail + `}}}}}`
		calls := 0
		c := chatReadClient(func(*http.Request) (*http.Response, error) { calls++; return chatReadResponse(body), nil })
		conv, err := c.GetXChatConversation(context.Background(), "123:456", "123", "456")
		if err != nil || calls != 1 || !reflect.DeepEqual(conv, chatReadSeal(c)) {
			t.Fatal("canonical one-to-one membership refused", err)
		}
		if _, err = c.GetXChatConversation(context.Background(), "123:456", "123", "789"); !errors.Is(err, ErrInvalidParams) || calls != 1 {
			t.Fatal("non-member pair dispatched")
		}
	}
}
