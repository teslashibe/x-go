package x

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// Target IDs match the synthetic list fixtures.
const (
	typedListID    = "1000000000000000910"
	typedNewListID = "1000000000000000911"
)

var typedListInput = ListInput{Name: "filler list 910", Description: "filler text 910", Private: true}

type listWriteCase struct {
	op     listWriteOp
	call   func(*Client, context.Context) (WriteResult, error)
	vars   map[string]any // the exact variables the request carries
	target string         // TargetID on success
	ack    string         // synthetic fixture with the provisional ack
}

var listWriteCases = []listWriteCase{
	{
		op:   createListOp,
		call: func(c *Client, ctx context.Context) (WriteResult, error) { return c.CreateList(ctx, typedListInput) },
		vars: map[string]any{"isPrivate": true, "name": "filler list 910", "description": "filler text 910"},
		// The new list's ID comes from the response.
		target: typedNewListID, ack: "list_create_synthetic.json",
	},
	{
		op: updateListOp,
		call: func(c *Client, ctx context.Context) (WriteResult, error) {
			return c.UpdateList(ctx, typedListID, typedListInput)
		},
		vars:   map[string]any{"listId": typedListID, "isPrivate": true, "name": "filler list 910", "description": "filler text 910"},
		target: typedListID, ack: "list_update_synthetic.json",
	},
	{
		op:   deleteListOp,
		call: func(c *Client, ctx context.Context) (WriteResult, error) { return c.DeleteList(ctx, typedListID) },
		vars: map[string]any{"listId": typedListID}, target: typedListID, ack: "list_delete_synthetic.json",
	},
	{
		op: addListMemberOp,
		call: func(c *Client, ctx context.Context) (WriteResult, error) {
			return c.AddListMember(ctx, typedListID, typedUserID)
		},
		vars: map[string]any{"listId": typedListID, "userId": typedUserID}, target: typedListID, ack: "list_member_synthetic.json",
	},
	{
		op: removeListMemberOp,
		call: func(c *Client, ctx context.Context) (WriteResult, error) {
			return c.RemoveListMember(ctx, typedListID, typedUserID)
		},
		vars: map[string]any{"listId": typedListID, "userId": typedUserID}, target: typedListID, ack: "list_member_synthetic.json",
	},
	{
		op:   subscribeListOp,
		call: func(c *Client, ctx context.Context) (WriteResult, error) { return c.SubscribeList(ctx, typedListID) },
		vars: map[string]any{"listId": typedListID}, target: typedListID, ack: "list_subscribe_synthetic.json",
	},
	{
		op:   unsubscribeListOp,
		call: func(c *Client, ctx context.Context) (WriteResult, error) { return c.UnsubscribeList(ctx, typedListID) },
		vars: map[string]any{"listId": typedListID}, target: typedListID, ack: "list_unsubscribe_synthetic.json",
	},
}

// listQueryID is the query id a test supplies for an operation, as a Live
// capture would add it to the defaults.
func listQueryID(op listWriteOp) string { return "fixture-" + op.operation }

// listWriteClient is recordingClient with a query id for every list
// mutation.
func listWriteClient(t *testing.T, respond func(http.ResponseWriter, *http.Request)) (*Client, func() []seenRequest) {
	t.Helper()
	c, seen := recordingClient(t, respond)
	for _, op := range listWriteOps {
		c.queryIDs[op.operation] = listQueryID(op)
	}
	return c, seen
}

// checkListRequest asserts the one request a list mutation sent.
func checkListRequest(t *testing.T, tc listWriteCase, req seenRequest) {
	t.Helper()
	if req.method != http.MethodPost || req.contentType != "application/json" {
		t.Fatalf("%s: request %s %q, want a JSON POST", tc.op.method, req.method, req.contentType)
	}
	if want := "/i/api/graphql/" + listQueryID(tc.op) + "/" + tc.op.operation; req.path != want {
		t.Fatalf("%s: path %s, want %s", tc.op.method, req.path, want)
	}
	var payload struct {
		Variables map[string]any `json:"variables"`
		QueryID   string         `json:"queryId"`
	}
	if err := json.Unmarshal([]byte(req.body), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.QueryID != listQueryID(tc.op) || len(payload.Variables) != len(tc.vars) {
		t.Fatalf("%s: payload %+v, want variables %v", tc.op.method, payload, tc.vars)
	}
	for k, v := range tc.vars {
		if payload.Variables[k] != v {
			t.Fatalf("%s: variables %v, want %v", tc.op.method, payload.Variables, tc.vars)
		}
	}
}

// TestListWritesSendOneRequest pins the request count: with the shipped
// defaults no list mutation has a query id, so each returns the typed
// query-id-unavailable error and sends nothing; with a query id present each
// sends exactly one request, never retried, carrying exactly its variables.
func TestListWritesSendOneRequest(t *testing.T) {
	ctx := context.Background()
	for _, op := range listWriteOps {
		if qid, ok := defaultQueryIDs[op.operation]; ok {
			t.Fatalf("%s ships query id %q; query ids enter the defaults only from a Live capture", op.operation, qid)
		}
	}
	if len(listWriteCases) != len(listWriteOps) {
		t.Fatalf("cases %d, ops %d", len(listWriteCases), len(listWriteOps))
	}
	for _, tc := range listWriteCases {
		t.Run(tc.op.method+"/no_query_id", func(t *testing.T) {
			c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json", `{"data":{}}`))
			res, err := tc.call(c, ctx)
			oe := outcomeOf(t, tc.op.method, res, err)
			wantDefinite(t, oe, true)
			if !errors.Is(err, ErrQueryIDUnavailable) || len(seen()) != 0 {
				t.Fatalf("error %v requests %d, want ErrQueryIDUnavailable and none", err, len(seen()))
			}
		})
		t.Run(tc.op.method+"/query_id", func(t *testing.T) {
			c, seen := listWriteClient(t, respondWith(http.StatusServiceUnavailable, "text/plain", "upstream"))
			res, err := tc.call(c, ctx)
			oe := outcomeOf(t, tc.op.method, res, err)
			wantAmbiguous(t, oe)
			reqs := seen()
			if len(reqs) != 1 || oe.Status != http.StatusServiceUnavailable {
				t.Fatalf("requests %d status %d, want 1 (never retried) and 503", len(reqs), oe.Status)
			}
			checkListRequest(t, tc, reqs[0])
		})
	}
}

// TestListWritesRefuseRedirect: a redirect is ambiguous and never followed,
// a dropped connection after sending is ambiguous, and the shared client is
// unchanged.
func TestListWritesRefuseRedirect(t *testing.T) {
	ctx := context.Background()
	for _, tc := range listWriteCases {
		c, seen := listWriteClient(t, func(rw http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/elsewhere" {
				rw.WriteHeader(http.StatusOK)
				return
			}
			http.Redirect(rw, r, "/elsewhere", http.StatusFound)
		})
		shared := c.httpClient
		res, err := tc.call(c, ctx)
		oe := outcomeOf(t, tc.op.method, res, err)
		wantAmbiguous(t, oe)
		if oe.Status != http.StatusFound || !errors.Is(err, errRedirectRefused) || len(seen()) != 1 {
			t.Fatalf("%s: error %v status %d requests %d, want refused 302 and 1 request", tc.op.method, err, oe.Status, len(seen()))
		}
		if c.httpClient != shared || c.httpClient.CheckRedirect != nil {
			t.Fatalf("%s: shared http.Client was modified", tc.op.method)
		}

		var received atomic.Int32
		c = newServerClient(t, func(rw http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			received.Add(1)
			if conn, _, err := rw.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
		})
		c.maxRetries = 3
		c.queryIDs[tc.op.operation] = listQueryID(tc.op)
		res, err = tc.call(c, ctx)
		oe = outcomeOf(t, tc.op.method, res, err)
		wantAmbiguous(t, oe)
		if oe.Status != 0 || received.Load() != 1 {
			t.Fatalf("%s reset: status %d requests %d, want 0 and 1", tc.op.method, oe.Status, received.Load())
		}
	}
}

// TestListWritesEmptyDataAmbiguous: {"data":{}} and every other clean
// response are ambiguous while no capture confirms the ack, including a
// response with the provisional ack shape; a bare 404 is ambiguous and
// ErrQueryIDStale, never ErrNotFound.
func TestListWritesEmptyDataAmbiguous(t *testing.T) {
	ctx := context.Background()
	for _, tc := range listWriteCases {
		bodies := []string{`{"data":{}}`, `{"data":null}`, `{}`, `not json`, string(fixture(t, tc.ack))}
		for _, body := range bodies {
			c, seen := listWriteClient(t, respondWith(http.StatusOK, "application/json", body))
			res, err := tc.call(c, ctx)
			oe := outcomeOf(t, tc.op.method, res, err)
			wantAmbiguous(t, oe)
			if oe.Status != http.StatusOK || len(seen()) != 1 {
				t.Fatalf("%s %s: status %d requests %d", tc.op.method, body, oe.Status, len(seen()))
			}
		}
		c, seen := listWriteClient(t, respondWith(http.StatusNotFound, "", ""))
		res, err := tc.call(c, ctx)
		oe := outcomeOf(t, tc.op.method, res, err)
		wantAmbiguous(t, oe)
		if !errors.Is(err, ErrQueryIDStale) || errors.Is(err, ErrNotFound) || oe.Status != 404 || len(seen()) != 1 {
			t.Fatalf("%s bare 404: %v %+v requests %d", tc.op.method, err, oe, len(seen()))
		}
	}
}

// TestListWriteAckRules fixture-tests the provisional ack shapes as a Live
// capture would register them: applied only with no errors and the ack
// root; CreateList reports the new list's ID and an ack without one is
// ambiguous; an ack naming another list is ambiguous.
func TestListWriteAckRules(t *testing.T) {
	ctx := context.Background()
	for _, op := range listWriteOps {
		if op.ackCaptured {
			t.Errorf("%s has a captured ack; acks are captured only from recorded provider responses", op.method)
		}
	}
	targetFor := func(op listWriteOp) string {
		if op.ack == ackNewList {
			return ""
		}
		return typedListID
	}
	for _, tc := range listWriteCases {
		op := tc.op
		op.ackCaptured = true
		ack := string(fixture(t, tc.ack))
		type ackCase struct {
			body    string
			applied bool
		}
		root := `{"data":{"` + op.ackRoot + `":`
		cases := []ackCase{
			{ack, true},
			{`{"data":{}}`, false},
			{root + `null}}`, false},
			{root + `{}}}`, false},
			{`{"data":{"other":"Done"}}`, false},
			{string(withErrors(t, []byte(ack), `[{"message":"x","code":139}]`)), false},
		}
		if op.ack != ackValue {
			// A list ack without a valid list ID is ambiguous.
			cases = append(cases, ackCase{root + `{"name":"filler list 910"}}}`, false}, ackCase{root + `{"id_str":"0"}}}`, false})
		}
		if op.ack == ackSameList {
			cases = append(cases, ackCase{root + `{"id_str":"` + typedNewListID + `"}}}`, false})
		}
		for _, cs := range cases {
			c, seen := listWriteClient(t, respondWith(http.StatusOK, "application/json", cs.body))
			res, err := c.listWrite(ctx, op, targetFor(op), tc.vars)
			if cs.applied {
				if err != nil || res != (WriteResult{Operation: op.method, TargetID: tc.target, State: WriteApplied}) {
					t.Fatalf("%s %s: %+v %v, want applied with target %s", op.method, cs.body, res, err, tc.target)
				}
			} else {
				wantAmbiguous(t, outcomeOf(t, op.method, res, err))
			}
			if len(seen()) != 1 {
				t.Fatalf("%s %s: requests %d", op.method, cs.body, len(seen()))
			}
		}
	}
}

// TestListWritesThrottling: HTTP 429 and X code 88 are definite rate limits
// with the cooldown recorded, sent once and never retried.
func TestListWritesThrottling(t *testing.T) {
	ctx := context.Background()
	responses := []struct {
		name     string
		status   int
		body     string
		wantCode int
	}{
		{"http_429", http.StatusTooManyRequests, `{"errors":[{"code":88,"message":"Rate limit exceeded"}]}`, 88},
		{"http_429_empty", http.StatusTooManyRequests, ``, 0},
		{"code_88_200", http.StatusOK, `{"errors":[{"code":88,"message":"Rate limit exceeded"}]}`, 88},
	}
	for _, tc := range listWriteCases {
		for _, resp := range responses {
			c, seen := listWriteClient(t, respondWith(resp.status, "application/json", resp.body, "Retry-After", "120"))
			res, err := tc.call(c, ctx)
			oe := outcomeOf(t, tc.op.method, res, err)
			wantDefinite(t, oe, false)
			var rle *RateLimitError
			if !errors.As(err, &rle) || rle.Wait != 120*time.Second || oe.Status != resp.status || oe.Code != resp.wantCode {
				t.Fatalf("%s %s: %v %+v, want *RateLimitError 120s", tc.op.method, resp.name, err, oe)
			}
			if !c.RateLimit().IsLimited() || len(seen()) != 1 {
				t.Fatalf("%s %s: limited %v requests %d", tc.op.method, resp.name, c.RateLimit().IsLimited(), len(seen()))
			}
		}
	}
}

// TestListWritesNotAttempted: invalid input or a cancelled context sends
// nothing, even with a query id present.
func TestListWritesNotAttempted(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range listWriteCases {
		c, seen := listWriteClient(t, respondWith(http.StatusOK, "application/json", `{}`))
		res, err := tc.call(c, cancelled)
		wantDefinite(t, outcomeOf(t, tc.op.method, res, err), true)
		if !errors.Is(err, context.Canceled) || !errors.Is(err, errWriteNotAttempted) || len(seen()) != 0 {
			t.Fatalf("%s cancelled: %v requests %d", tc.op.method, err, len(seen()))
		}
	}

	ctx := context.Background()
	c, seen := listWriteClient(t, respondWith(http.StatusOK, "application/json", `{}`))
	bad := []string{"", "abc", "0", "012", " 1", "123456789012345678901"}
	var calls []struct {
		method string
		call   func() (WriteResult, error)
	}
	add := func(method string, call func() (WriteResult, error)) {
		calls = append(calls, struct {
			method string
			call   func() (WriteResult, error)
		}{method, call})
	}
	for _, in := range []ListInput{{}, {Name: "   "}, {Name: "\xff"}, {Name: "ok", Description: "\xff"}} {
		add("CreateList", func() (WriteResult, error) { return c.CreateList(ctx, in) })
		add("UpdateList", func() (WriteResult, error) { return c.UpdateList(ctx, typedListID, in) })
	}
	for _, id := range bad {
		add("UpdateList", func() (WriteResult, error) { return c.UpdateList(ctx, id, typedListInput) })
		add("DeleteList", func() (WriteResult, error) { return c.DeleteList(ctx, id) })
		add("AddListMember", func() (WriteResult, error) { return c.AddListMember(ctx, id, typedUserID) })
		add("AddListMember", func() (WriteResult, error) { return c.AddListMember(ctx, typedListID, id) })
		add("RemoveListMember", func() (WriteResult, error) { return c.RemoveListMember(ctx, id, typedUserID) })
		add("RemoveListMember", func() (WriteResult, error) { return c.RemoveListMember(ctx, typedListID, id) })
		add("SubscribeList", func() (WriteResult, error) { return c.SubscribeList(ctx, id) })
		add("UnsubscribeList", func() (WriteResult, error) { return c.UnsubscribeList(ctx, id) })
	}
	for _, cl := range calls {
		res, err := cl.call()
		wantDefinite(t, outcomeOf(t, cl.method, res, err), true)
		if !errors.Is(err, ErrInvalidParams) {
			t.Fatalf("%s: %v", cl.method, err)
		}
	}
	if n := len(seen()); n != 0 {
		t.Fatalf("%d requests for invalid input", n)
	}
}

// TestListWriteRejectionsKeepClassification: known X codes are definite with
// the code recorded; an unknown code on a 200 is ambiguous; errors next to
// data are ambiguous.
func TestListWriteRejectionsKeepClassification(t *testing.T) {
	ctx := context.Background()
	for _, tc := range listWriteCases {
		c, _ := listWriteClient(t, respondWith(http.StatusOK, "application/json", `{"errors":[{"code":226,"message":"automated"}]}`))
		res, err := tc.call(c, ctx)
		oe := outcomeOf(t, tc.op.method, res, err)
		wantDefinite(t, oe, false)
		if !errors.Is(err, ErrAutomatedRequest) || oe.Code != 226 {
			t.Fatalf("%s 226: %v %+v", tc.op.method, err, oe)
		}
		c, _ = listWriteClient(t, respondWith(http.StatusOK, "application/json", `{"errors":[{"code":9999,"message":"something new"}]}`))
		res, err = tc.call(c, ctx)
		if oe := outcomeOf(t, tc.op.method, res, err); oe.Code != 9999 {
			t.Fatalf("%s unknown code: %+v", tc.op.method, oe)
		} else {
			wantAmbiguous(t, oe)
		}
		c, _ = listWriteClient(t, respondWith(http.StatusOK, "application/json", string(withErrors(t, fixture(t, tc.ack), `[{"code":144,"message":"not found"}]`))))
		res, err = tc.call(c, ctx)
		wantAmbiguous(t, outcomeOf(t, tc.op.method, res, err))
	}
}
