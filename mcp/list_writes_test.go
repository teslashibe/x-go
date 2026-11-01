package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	x "github.com/teslashibe/x-go"
	xmcp "github.com/teslashibe/x-go/mcp"
)

var listWriteTools = map[string]struct {
	method, op, input string
}{
	"x_create_list":        {"CreateList", "CreateList", `{"name":"l","description":"d","private":true}`},
	"x_update_list":        {"UpdateList", "UpdateList", `{"list_id":"1001","name":"l"}`},
	"x_delete_list":        {"DeleteList", "DeleteList", `{"list_id":"1001"}`},
	"x_add_list_member":    {"AddListMember", "ListAddMember", `{"list_id":"1001","user_id":"1002"}`},
	"x_remove_list_member": {"RemoveListMember", "ListRemoveMember", `{"list_id":"1001","user_id":"1002"}`},
	"x_subscribe_list":     {"SubscribeList", "ListSubscribe", `{"list_id":"1001"}`},
	"x_unsubscribe_list":   {"UnsubscribeList", "ListUnsubscribe", `{"list_id":"1001"}`},
}

// TestListWriteToolsRefuseWithoutQueryID: each list mutation tool wraps its
// method, and with the shipped query ids it refuses with the typed
// query-id-unavailable error and sends no request.
func TestListWriteToolsRefuseWithoutQueryID(t *testing.T) {
	c, reads := fixtureClient(t, nil)
	for name, tc := range listWriteTools {
		tl := tool(t, name)
		if tl.WrapsMethod != tc.method {
			t.Fatalf("%s wraps %s, want %s", name, tl.WrapsMethod, tc.method)
		}
		out, err := tl.Invoke(context.Background(), c, json.RawMessage(tc.input))
		var oe *x.OutcomeError
		if out != nil || !errors.As(err, &oe) || !oe.NotAttempted || oe.Operation != tc.method || !errors.Is(err, x.ErrQueryIDUnavailable) {
			t.Fatalf("%s: %v %v, want a NotAttempted ErrQueryIDUnavailable", name, out, err)
		}
	}
	if got := reads.Load(); got != 0 {
		t.Fatalf("requests %d, want 0", got)
	}
}

// TestListWriteToolsSendOnce: with a query id supplied each tool sends one
// request, and a response no capture confirms is an ambiguous outcome,
// never a success.
func TestListWriteToolsSendOnce(t *testing.T) {
	ids := map[string]string{}
	serve := map[string]string{}
	for _, tc := range listWriteTools {
		ids[tc.op] = "fixture-" + tc.op
		serve[tc.op] = `{"data":{}}`
	}
	c, reads := fixtureClient(t, serve, x.WithQueryIDs(ids))
	for name, tc := range listWriteTools {
		before := reads.Load()
		out, err := tool(t, name).Invoke(context.Background(), c, json.RawMessage(tc.input))
		var oe *x.OutcomeError
		if out != nil || !errors.As(err, &oe) || !oe.Ambiguous || oe.Operation != tc.method {
			t.Fatalf("%s: %v %v, want an ambiguous outcome", name, out, err)
		}
		if got := reads.Load() - before; got != 1 {
			t.Fatalf("%s: requests %d, want 1", name, got)
		}
	}
}

// TestListWriteToolsComeLast keeps every older tool in its order.
func TestListWriteToolsComeLast(t *testing.T) {
	tools := (xmcp.Provider{}).Tools()
	for _, tl := range tools[len(tools)-len(listWriteTools):] {
		if _, ok := listWriteTools[tl.Name]; !ok {
			t.Fatalf("tool %s sits among the list mutation tools", tl.Name)
		}
	}
}
