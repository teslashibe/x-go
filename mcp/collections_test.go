package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
	xmcp "github.com/teslashibe/x-go/mcp"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// fixtureClient builds a client over a fake transport: session bootstrap
// answers from fixtures, transaction bootstrap fails offline, and every
// other GraphQL operation is answered by serve and counted.
func fixtureClient(t *testing.T, serve map[string]string, opts ...x.Option) (*x.Client, *atomic.Int64) {
	t.Helper()
	var reads atomic.Int64
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/Viewer"):
			return jsonResponse(`{"data":{"viewer":{"user_results":{"result":{"rest_id":"7"}}}}}`), nil
		case strings.HasSuffix(r.URL.Path, "/UserByRestId"):
			return jsonResponse(`{"data":{"user":{"result":{"rest_id":"7","legacy":{"screen_name":"u7"}}}}}`), nil
		case !strings.Contains(r.URL.Path, "/graphql/"):
			return nil, errors.New("offline")
		}
		reads.Add(1)
		for op, body := range serve {
			if strings.HasSuffix(r.URL.Path, "/"+op) {
				return jsonResponse(body), nil
			}
		}
		t.Errorf("unexpected request %s", r.URL.Path)
		return nil, errors.New("unexpected request")
	})}
	opts = append([]x.Option{x.WithHTTPClient(hc), x.WithMinRequestGap(0), x.WithRetry(1, 0)}, opts...)
	c, err := (x.Session{AuthToken: "auth", CT0: "csrf", Twid: "u=7"}).NewClient(context.Background(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c, &reads
}

func tool(t *testing.T, name string) mcptool.Tool {
	t.Helper()
	for _, tl := range (xmcp.Provider{}).Tools() {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("tool %s not registered", name)
	return mcptool.Tool{}
}

var collectionTools = map[string]struct {
	method, op, input string
}{
	"x_get_bookmarks":        {"GetBookmarksPage", "Bookmarks", `{}`},
	"x_get_liked_posts":      {"GetLikedPostsPage", "Likes", `{"user_id":"7"}`},
	"x_get_muted_users":      {"GetMutedUsersPage", "MutedAccounts", `{}`},
	"x_get_blocked_users":    {"GetBlockedUsersPage", "BlockedAccountsAll", `{}`},
	"x_get_owned_lists":      {"GetOwnedListsPage", "ListOwnerships", `{"user_id":"7"}`},
	"x_get_subscribed_lists": {"GetSubscribedListsPage", "ListSubscriptions", `{}`},
}

// TestCollectionToolsRefuseWithoutQueryID: each new tool wraps its method,
// and with the shipped query ids it refuses with the typed error and sends no
// request.
func TestCollectionToolsRefuseWithoutQueryID(t *testing.T) {
	c, reads := fixtureClient(t, nil)
	for name, tc := range collectionTools {
		tl := tool(t, name)
		if tl.WrapsMethod != tc.method {
			t.Fatalf("%s wraps %s, want %s", name, tl.WrapsMethod, tc.method)
		}
		_, err := tl.Invoke(context.Background(), c, json.RawMessage(tc.input))
		var unavailable *x.QueryIDUnavailableError
		if !errors.As(err, &unavailable) || unavailable.Operation != tc.op {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if got := reads.Load(); got != 0 {
		t.Fatalf("requests %d, want 0", got)
	}
}

// TestCollectionToolsReturnCoverage: with a query id supplied, the tools
// return the usual page plus the provider page's coverage.
func TestCollectionToolsReturnCoverage(t *testing.T) {
	const list = `{"id_str":"1001","name":"l","mode":"Public","user":{"rest_id":"7","legacy":{"screen_name":"u7","name":"U"}}}`
	body := func(path, content string) string {
		inner := `{"timeline":{"instructions":[{"type":"TimelineAddEntries","entries":[` +
			`{"entryId":"item-1","content":{"itemContent":` + content + `}},` +
			`{"entryId":"cursor-bottom-0","content":{"cursorType":"Bottom","value":"c2"}}]}]}}`
		parts := strings.Split(path, ".")
		for i := len(parts) - 1; i >= 0; i-- {
			inner = `{"` + parts[i] + `":` + inner + `}`
		}
		return `{"data":` + inner + `}`
	}
	c, reads := fixtureClient(t, map[string]string{
		"ListSubscriptions": body("viewer.list_subscriptions_timeline", `{"list":`+list+`}`),
		"MutedAccounts":     body("viewer.muting_timeline", `{"user_results":{"result":{"rest_id":"1002","core":{"screen_name":"u1002"}}}}`),
	}, x.WithQueryIDs(map[string]string{"ListSubscriptions": "fixture-lists", "MutedAccounts": "fixture-muted"}))

	out, err := tool(t, "x_get_subscribed_lists").Invoke(context.Background(), c, json.RawMessage(`{"count":5}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	want := `{"items":[{"id":"1001","name":"l","memberCount":0,"ownerId":"7","ownerName":"U","isPrivate":false,"ownerScreenName":"u7"}],` +
		`"next_cursor":"c2","coverage":{"end":"more","dropped":0,"partialErrors":0}}`
	if string(raw) != want {
		t.Fatalf("x_get_subscribed_lists\n got %s\nwant %s", raw, want)
	}

	out, err = tool(t, "x_get_muted_users").Invoke(context.Background(), c, json.RawMessage(`{"cursor":"c2"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(out)
	if !strings.Contains(string(raw), `"coverage":{"end":"stalled","dropped":0,"partialErrors":0}`) || !strings.Contains(string(raw), `"id":"1002"`) {
		t.Fatalf("x_get_muted_users %s", raw)
	}
	if got := reads.Load(); got != 2 {
		t.Fatalf("requests %d, want 2", got)
	}
}

// TestExistingPageToolsUnchanged: the older page tools keep their answer
// bytes; coverage appears only on the new tools.
func TestExistingPageToolsUnchanged(t *testing.T) {
	c, _ := fixtureClient(t, map[string]string{
		"Followers": `{"data":{"user":{"result":{"timeline":{"timeline":{"instructions":[{"type":"TimelineAddEntries","entries":[` +
			`{"entryId":"user-1","content":{"itemContent":{"user_results":{"result":{"rest_id":"1002","core":{"screen_name":"u1002","name":"N"}}}}}},` +
			`{"entryId":"cursor-bottom-0","content":{"cursorType":"Bottom","value":"c2"}}]}]}}}}}}`,
	})
	out, err := tool(t, "x_get_followers").Invoke(context.Background(), c, json.RawMessage(`{"user_id":"7"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	want := `{"items":[{"id":"1002","screenName":"u1002","name":"N","followersCount":0,"followingCount":0,"tweetCount":0,"listedCount":0,"verified":false,"isBlueVerified":false,"createdAt":"0001-01-01T00:00:00Z"}],"next_cursor":"c2"}`
	if string(raw) != want {
		t.Fatalf("x_get_followers\n got %s\nwant %s", raw, want)
	}
}

// TestCollectionToolsComeLast keeps the older tools in their order.
func TestCollectionToolsComeLast(t *testing.T) {
	tools := (xmcp.Provider{}).Tools()
	tail := tools[len(tools)-len(collectionTools):]
	for _, tl := range tail {
		if _, ok := collectionTools[tl.Name]; !ok {
			t.Fatalf("tool %s sits among the collection tools", tl.Name)
		}
	}
	if len(tools) != 45 {
		t.Fatalf("tools %d, want 45 (39 existing + 6 collection reads)", len(tools))
	}
}
