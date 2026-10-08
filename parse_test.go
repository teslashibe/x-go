package x

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Fixture IDs come from testdata/graphql (see sanitize.py there): the real
// post's ID, which is also the focal post of tweet_detail_synthetic.json, and
// its author's handle, which is the real profile's.
const (
	fixtureFocalID    = "1000000000000000076"
	fixtureScreenName = "u25"
)

// newServerClient returns a test Client whose requests are served by handler
// through an httptest server.
func newServerClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := srv.Client().Transport
	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host, r.Host = target.Scheme, target.Host, target.Host
		return transport.RoundTrip(r)
	}))
	c.queryIDs = make(map[string]string, len(defaultQueryIDs))
	for k, v := range defaultQueryIDs {
		c.queryIDs[k] = v
	}
	c.features = map[string]bool{}
	c.maxRetries = 1
	return c
}

// serveBody answers GraphQL operation op with a recorded 200 body.
func serveBody(t *testing.T, op string, body []byte) *Client {
	t.Helper()
	return newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/"+op) {
			t.Errorf("request path %s, want operation %s", r.URL.Path, op)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "graphql", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// withErrors adds a GraphQL errors array to body, keeping its data.
func withErrors(t *testing.T, body []byte, errs string) []byte {
	t.Helper()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["errors"] = json.RawMessage(errs)
	out, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type parseCase struct {
	op     string
	client func(context.Context, *Client) (any, error)
	parse  func([]byte) (any, error)
}

var parseCases = map[string]parseCase{
	"search": {
		op: "SearchTimeline",
		client: func(ctx context.Context, c *Client) (any, error) {
			return c.SearchTweetsPage(ctx, "news", 20, "")
		},
		parse: func(b []byte) (any, error) { return ParseSearchTimeline(b) },
	},
	"thread": {
		op: "TweetDetail",
		client: func(ctx context.Context, c *Client) (any, error) {
			return c.GetTweetDetail(ctx, fixtureFocalID)
		},
		parse: func(b []byte) (any, error) { return ParseTweetDetail(b, fixtureFocalID) },
	},
	"profile": {
		op: "UserByScreenName",
		client: func(ctx context.Context, c *Client) (any, error) {
			return c.GetProfile(ctx, fixtureScreenName)
		},
		parse: func(b []byte) (any, error) { return ParseUserByScreenName(b) },
	},
	"post": {
		op: "TweetResultByRestId",
		client: func(ctx context.Context, c *Client) (any, error) {
			return c.GetTweet(ctx, fixtureFocalID)
		},
		parse: func(b []byte) (any, error) { return ParseTweetResultByRestID(b) },
	},
}

// syntheticTweet is a minimal tweet_results.result object.
func syntheticTweet(id, conversation, extra string) string {
	return `{"__typename":"Tweet","rest_id":"` + id + `",` +
		`"core":{"user_results":{"result":{"__typename":"User","rest_id":"7","core":{"screen_name":"u7","name":"User 7"},"legacy":{"followers_count":42}}}},` +
		`"views":{"count":"10","state":"EnabledWithCount"},` +
		`"legacy":{"full_text":"text ` + id + `","conversation_id_str":"` + conversation + `","created_at":"Tue Oct 06 23:04:45 +0000 2026"` + extra + `}}`
}

func syntheticBodies() map[string]string {
	focal := syntheticTweet(fixtureFocalID, fixtureFocalID, "")
	reply := syntheticTweet("9001", fixtureFocalID, `,"in_reply_to_status_id_str":"`+fixtureFocalID+`"`)
	return map[string]string{
		"search": `{"data":{"search_by_raw_query":{"search_timeline":{"timeline":{"instructions":[{"type":"TimelineAddEntries","entries":[` +
			`{"entryId":"tweet-` + fixtureFocalID + `","content":{"entryType":"TimelineTimelineItem","itemContent":{"tweet_results":{"result":` + focal + `}}}},` +
			`{"entryId":"cursor-bottom-0","content":{"entryType":"TimelineTimelineCursor","cursorType":"Bottom","value":"next"}}]}]}}}}}`,
		"thread": `{"data":{"threaded_conversation_with_injections_v2":{"instructions":[{"type":"TimelineClearCache"},{"type":"TimelineAddEntries","entries":[` +
			`{"entryId":"tweet-` + fixtureFocalID + `","content":{"entryType":"TimelineTimelineItem","itemContent":{"tweet_results":{"result":` + focal + `}}}},` +
			`{"entryId":"conversationthread-9001","content":{"entryType":"TimelineTimelineModule","items":[{"item":{"itemContent":{"tweet_results":{"result":` + reply + `}}}}]}}]}]}}}`,
		"profile": `{"data":{"user":{"result":{"__typename":"User","rest_id":"7","core":{"screen_name":"` + fixtureScreenName + `","name":"User 7","created_at":"Mon Aug 03 11:56:31 +0000 2026"},"legacy":{"followers_count":42,"friends_count":3,"description":"bio"}}}}}`,
		"post":    `{"data":{"tweetResult":{"result":` + focal + `}}}`,
	}
}

// realFixtures lists the recorded bodies per operation. All are sanitized
// real Scarlett pages except tweet_detail_synthetic.json, which sanitize.py
// builds from real post objects (no real TweetDetail page is recorded yet).
var realFixtures = map[string][]string{
	"search":  {"search_timeline_hot.json", "search_timeline_first.json", "search_timeline_empty.json"},
	"thread":  {"tweet_detail_synthetic.json"},
	"profile": {"user_by_screen_name.json"},
	"post":    {"tweet_result_by_rest_id.json"},
}

func TestParsersMatchClientMethods(t *testing.T) {
	ctx := context.Background()
	for name, tc := range parseCases {
		bodies := map[string][]byte{"synthetic": []byte(syntheticBodies()[name])}
		for _, file := range realFixtures[name] {
			bodies[file] = fixture(t, file)
		}
		for label, body := range bodies {
			t.Run(name+"/"+label, func(t *testing.T) {
				fromClient, err := tc.client(ctx, serveBody(t, tc.op, body))
				if err != nil {
					t.Fatalf("client: %v", err)
				}
				parsed, err := tc.parse(body)
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				if !reflect.DeepEqual(fromClient, parsed) {
					t.Fatalf("client and parser differ:\nclient %#v\nparser %#v", fromClient, parsed)
				}
			})
		}
	}
}

func TestParseSearchTimelineRealPages(t *testing.T) {
	hot, err := ParseSearchTimeline(fixture(t, "search_timeline_hot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(hot.Tweets) != 7 || !hot.HasNext || hot.NextCursor == "" {
		t.Fatalf("hot page: %d tweets, hasNext %v, cursor %q", len(hot.Tweets), hot.HasNext, hot.NextCursor)
	}
	var wrapped, quotes int
	for _, tw := range hot.Tweets {
		if tw.ID == "" || tw.AuthorID == "" || tw.AuthorScreenName == "" || tw.CreatedAt.IsZero() {
			t.Errorf("incomplete tweet %#v", tw)
		}
		if !tw.ViewCountKnown || tw.ViewCount <= 0 {
			t.Errorf("tweet %s: views %d known %v", tw.ID, tw.ViewCount, tw.ViewCountKnown)
		}
		if tw.AuthorFollowersCount == nil {
			t.Errorf("tweet %s: author followers missing", tw.ID)
		}
		if tw.Raw == nil || tw.Raw.SchemaVersion != 1 || tw.Raw.Provider != "x_graphql" {
			t.Errorf("tweet %s: raw envelope %#v", tw.ID, tw.Raw)
		}
		var typed struct {
			Typename string `json:"__typename"`
		}
		if err := json.Unmarshal(tw.Raw.Result, &typed); err != nil {
			t.Fatal(err)
		}
		if typed.Typename == "TweetWithVisibilityResults" {
			wrapped++
		}
		if tw.IsQuote {
			quotes++
		}
	}
	if wrapped != 2 || quotes == 0 {
		t.Fatalf("visibility-wrapped %d (want 2), quotes %d", wrapped, quotes)
	}

	first, err := ParseSearchTimeline(fixture(t, "search_timeline_first.json"))
	if err != nil {
		t.Fatal(err)
	}
	unknown := 0
	for _, tw := range first.Tweets {
		if !tw.ViewCountKnown {
			unknown++
			if tw.ViewCount != 0 {
				t.Errorf("tweet %s: unknown views but ViewCount %d", tw.ID, tw.ViewCount)
			}
		}
	}
	if len(first.Tweets) != 5 || unknown != 2 {
		t.Fatalf("first page: %d tweets, %d with unknown views; want 5 and 2", len(first.Tweets), unknown)
	}

	empty, err := ParseSearchTimeline(fixture(t, "search_timeline_empty.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Tweets) != 0 || !empty.HasNext {
		t.Fatalf("empty page = %#v", empty)
	}
}

func TestParseTweetDetailFixture(t *testing.T) {
	detail, err := ParseTweetDetail(fixture(t, "tweet_detail_synthetic.json"), fixtureFocalID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Tweet.ID != fixtureFocalID || len(detail.Ancestors) != 2 || len(detail.Replies) != 3 {
		t.Fatalf("focal %s with %d ancestors and %d replies; want %s with 2 and 3 (tombstone skipped)",
			detail.Tweet.ID, len(detail.Ancestors), len(detail.Replies), fixtureFocalID)
	}
	root, parent := detail.Ancestors[0], detail.Ancestors[1]
	if root.InReplyToID != "" || parent.InReplyToID != root.ID || detail.Tweet.InReplyToID != parent.ID {
		t.Fatalf("chain: root replies to %q, parent %s to %q, focal to %q", root.InReplyToID, parent.ID, parent.InReplyToID, detail.Tweet.InReplyToID)
	}
	for _, r := range detail.Replies {
		if r.InReplyToID != fixtureFocalID || r.ConversationID != root.ID {
			t.Errorf("reply %s: inReplyTo %s conversation %s", r.ID, r.InReplyToID, r.ConversationID)
		}
	}
}

func TestParseTweetDetailOrder(t *testing.T) {
	entry := func(id, conversation, parent string) string {
		extra := ""
		if parent != "" {
			extra = `,"in_reply_to_status_id_str":"` + parent + `"`
		}
		return `{"entryId":"tweet-` + id + `","content":{"entryType":"TimelineTimelineItem","itemContent":{"tweet_results":{"result":` + syntheticTweet(id, conversation, extra) + `}}}}`
	}
	module := func(ids ...string) string {
		items := make([]string, len(ids))
		for i, id := range ids {
			items[i] = `{"item":{"itemContent":{"tweet_results":{"result":` + syntheticTweet(id, "1", `,"in_reply_to_status_id_str":"3"`) + `}}}}`
		}
		return `{"entryId":"conversationthread-` + ids[0] + `","content":{"entryType":"TimelineTimelineModule","items":[` + strings.Join(items, ",") + `]}}`
	}
	ids := func(tweets []Tweet) []string {
		out := []string{}
		for _, tw := range tweets {
			out = append(out, tw.ID)
		}
		return out
	}
	cases := []struct {
		name               string
		entries            []string
		ancestors, replies []string
	}{
		{"root focal", []string{entry("3", "3", ""), module("4", "5"), module("6")}, []string{}, []string{"4", "5", "6"}},
		{"reply focal", []string{entry("1", "1", ""), entry("2", "1", "1"), entry("3", "1", "2"), module("4"), module("5", "6")}, []string{"1", "2"}, []string{"4", "5", "6"}},
		{"ancestors in a module", []string{module("1", "2"), entry("3", "1", "2"), module("4")}, []string{"1", "2"}, []string{"4"}},
	}
	for _, c := range cases {
		body := `{"data":{"threaded_conversation_with_injections_v2":{"instructions":[{"type":"TimelineAddEntries","entries":[` + strings.Join(c.entries, ",") + `]}]}}}`
		detail, err := ParseTweetDetail([]byte(body), "3")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if detail.Tweet.ID != "3" || !reflect.DeepEqual(ids(detail.Ancestors), c.ancestors) || !reflect.DeepEqual(ids(detail.Replies), c.replies) {
			t.Errorf("%s: focal %s ancestors %v replies %v; want ancestors %v replies %v",
				c.name, detail.Tweet.ID, ids(detail.Ancestors), ids(detail.Replies), c.ancestors, c.replies)
		}
		fromClient, err := serveBody(t, "TweetDetail", []byte(body)).GetTweetDetail(context.Background(), "3")
		if err != nil || !reflect.DeepEqual(fromClient, detail) {
			t.Errorf("%s: client %v (%v) differs from parser", c.name, fromClient, err)
		}
	}
}

func TestParseUserByScreenNameFixture(t *testing.T) {
	body := fixture(t, "user_by_screen_name.json")
	var recorded struct {
		Data struct {
			User struct {
				Result struct {
					Legacy struct {
						FollowersCount int `json:"followers_count"`
					} `json:"legacy"`
				} `json:"result"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &recorded); err != nil {
		t.Fatal(err)
	}
	u, err := ParseUserByScreenName(body)
	if err != nil {
		t.Fatal(err)
	}
	want := recorded.Data.User.Result.Legacy.FollowersCount
	if u.ScreenName != fixtureScreenName || u.ID == "" || want == 0 || u.FollowersCount != want || u.CreatedAt.IsZero() || u.Description == "" {
		t.Fatalf("user = %#v, want followers %d", u, want)
	}
}

func TestTweetViewAndFollowerFields(t *testing.T) {
	tweet := func(views, followers string) *Tweet {
		t.Helper()
		body := `{"data":{"tweetResult":{"result":{"__typename":"Tweet","rest_id":"5",` +
			`"core":{"user_results":{"result":{"rest_id":"7","legacy":{"screen_name":"u7"` + followers + `}}}},` +
			`"views":` + views + `,"legacy":{"full_text":"x"}}}}}`
		tw, err := ParseTweetResultByRestID([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		return tw
	}

	set := tweet(`{"count":"123","state":"EnabledWithCount"}`, `,"followers_count":42`)
	if !set.ViewCountKnown || set.ViewCount != 123 || set.AuthorFollowersCount == nil || *set.AuthorFollowersCount != 42 {
		t.Fatalf("set: views %d known %v followers %v", set.ViewCount, set.ViewCountKnown, set.AuthorFollowersCount)
	}
	zero := tweet(`{"count":"0","state":"EnabledWithCount"}`, `,"followers_count":0`)
	if !zero.ViewCountKnown || zero.AuthorFollowersCount == nil || *zero.AuthorFollowersCount != 0 {
		t.Fatalf("zero: known %v followers %v", zero.ViewCountKnown, zero.AuthorFollowersCount)
	}
	unset := tweet(`{"state":"Enabled"}`, ``)
	if unset.ViewCountKnown || unset.ViewCount != 0 || unset.AuthorFollowersCount != nil {
		t.Fatalf("unset: views %d known %v followers %v", unset.ViewCount, unset.ViewCountKnown, unset.AuthorFollowersCount)
	}
	bad := tweet(`{"count":"many","state":"EnabledWithCount"}`, ``)
	if bad.ViewCountKnown || bad.ViewCount != 0 {
		t.Fatalf("non-numeric: views %d known %v", bad.ViewCount, bad.ViewCountKnown)
	}

	// The fields survive JSON round trips with the documented names.
	out, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"viewCountKnown":true`) || !strings.Contains(string(out), `"authorFollowersCount":42`) {
		t.Fatalf("json = %s", out)
	}
	out, err = json.Marshal(unset)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "authorFollowersCount") {
		t.Fatalf("unknown followers serialized: %s", out)
	}
}

func TestParseEnvelopePartialErrors(t *testing.T) {
	// X reports unavailable embedded posts as NonFatal errors with a path
	// below the root field; the page itself is usable.
	const partial = `[{"message":"_Missing: No status found with that ID.","code":144,"kind":"NonFatal",` +
		`"path":["root","result","quoted_status_result","result"],"extensions":{"code":144,"kind":"NonFatal"}},` +
		`{"message":"_Missing: No status found with that ID.","path":["root","entries",3],"extensions":{"kind":"NonFatal"}}]`
	// A root-level error names the operation itself.
	const root = `[{"message":"Sorry, that page does not exist","code":34}]`
	ctx := context.Background()
	for name, tc := range parseCases {
		for _, file := range realFixtures[name] {
			t.Run(name+"/"+file, func(t *testing.T) {
				clean := fixture(t, file)
				want, err := tc.parse(clean)
				if err != nil {
					t.Fatal(err)
				}
				for _, errs := range []string{partial, root} {
					got, err := tc.parse(withErrors(t, clean, errs))
					if err != nil {
						t.Fatalf("data with errors must parse: %v", err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatal("errors changed the parsed data")
					}
				}
				// The client drops partial errors next to usable data …
				got, err := tc.client(ctx, serveBody(t, tc.op, withErrors(t, clean, partial)))
				if err != nil {
					t.Fatalf("client with partial errors: %v", err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("client and parser differ on a page with partial errors")
				}
				// … and fails on a root-level error.
				if _, err := tc.client(ctx, serveBody(t, tc.op, withErrors(t, clean, root))); !errors.Is(err, ErrNotFound) {
					t.Fatalf("client error = %v, want ErrNotFound", err)
				}
			})
		}
	}
}

func TestParseRootErrorsBesideEmptyData(t *testing.T) {
	ctx := context.Background()
	roots := map[string]string{
		"search": "search_by_raw_query", "thread": "threaded_conversation_with_injections_v2",
		"profile": "user", "post": "tweetResult",
	}
	codes := []struct {
		errs string
		want error
	}{
		{`[{"message":"Rate limit exceeded","code":88}]`, ErrRateLimited},
		{`[{"message":"Could not authenticate you","code":32}]`, ErrUnauthorized},
		{`[{"message":"Authorization: To protect our users from spam and other malicious activity, this account is temporarily locked.","code":326,"kind":"Permissions"}]`, ErrChallenge},
		{`[{"message":"Authorization: Denied by access control","extensions":{"code":37,"kind":"Permissions"}}]`, ErrRequestFailed},
	}
	for name, tc := range parseCases {
		for _, data := range []string{`{}`, `{"` + roots[name] + `":null}`, `{"` + roots[name] + `":{}}`} {
			for _, c := range codes {
				body := []byte(`{"data":` + data + `,"errors":` + c.errs + `}`)
				_, err := tc.parse(body)
				if !errors.Is(err, c.want) || (c.want != ErrNotFound && errors.Is(err, ErrNotFound)) {
					t.Errorf("parse %s %s: error = %v, want %v", name, body, err, c.want)
				}
				if _, cerr := tc.client(ctx, serveBody(t, tc.op, body)); !errors.Is(cerr, c.want) {
					t.Errorf("client %s %s: error = %v, want %v", name, body, cerr, c.want)
				}
			}
		}
	}
	// Without errors an empty data object still means the resource is absent.
	for _, name := range []string{"thread", "profile", "post"} {
		if _, err := parseCases[name].parse([]byte(`{"data":{}}`)); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: {\"data\":{}} error = %v, want ErrNotFound", name, err)
		}
	}
}

func TestParseEnvelopeErrorsOnly(t *testing.T) {
	cases := []struct {
		body string
		want error
	}{
		{`{"errors":[{"message":"Sorry, that page does not exist","code":34}]}`, ErrNotFound},
		{`{"data":null,"errors":[{"message":"Sorry, that page does not exist","code":34}]}`, ErrNotFound},
		{`{"data":null,"errors":[{"message":"User has been suspended.","code":63}]}`, ErrSuspended},
		{`{"errors":[{"message":"Rate limit exceeded","code":88}]}`, ErrRateLimited},
		{`{"errors":[{"message":"Could not authenticate you","code":32}]}`, ErrUnauthorized},
		{`{"errors":[{"message":"something new","code":999}]}`, ErrRequestFailed},
		{`{"errors":"unexpected"}`, ErrRequestFailed},
		{`{"data":null}`, ErrRequestFailed},
		{`{}`, ErrRequestFailed},
		{`not json`, ErrRequestFailed},
		{``, ErrRequestFailed},
	}
	for name, tc := range parseCases {
		for _, c := range cases {
			if _, err := tc.parse([]byte(c.body)); !errors.Is(err, c.want) {
				t.Errorf("%s(%q) error = %v, want %v", name, c.body, err, c.want)
			}
		}
	}
}

func TestParseErrorsNeverEchoBody(t *testing.T) {
	const secretish = "do-not-echo-this-body"
	for name, tc := range parseCases {
		_, err := tc.parse([]byte(`{"errors":[{"message":"x","code":999}],"` + secretish + `":`))
		if err == nil || strings.Contains(err.Error(), secretish) {
			t.Errorf("%s: error %v echoes the body", name, err)
		}
	}
}

func TestParseNotFoundAndSuspendedShapes(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		op   string
		body string
		want error
	}{
		{"post", "TweetResultByRestId", `{"data":{"tweetResult":{}}}`, ErrNotFound},
		{"post", "TweetResultByRestId", `{"data":{"tweetResult":{"result":{"__typename":"TweetTombstone","tombstone":{"text":{"text":"deleted"}}}}}}`, ErrNotFound},
		{"post", "TweetResultByRestId", `{"data":{"tweetResult":{"result":{"__typename":"TweetUnavailable","reason":"Suspended"}}}}`, ErrNotFound},
		{"post", "TweetResultByRestId", `{"data":{"tweetResult":{"result":{"__typename":"Tweet","legacy":{"full_text":"no id"}}}}}`, ErrNotFound},
		{"post", "TweetResultByRestId", `{"data":{"tweetResult":{"result":{"__typename":"TweetWithVisibilityResults"}}}}`, ErrNotFound},
		{"thread", "TweetDetail", `{"data":{}}`, ErrNotFound},
		{"thread", "TweetDetail", `{"data":{"threaded_conversation_with_injections_v2":{"instructions":[]}}}`, ErrNotFound},
		{"thread", "TweetDetail", `{"data":{"threaded_conversation_with_injections_v2":{"instructions":[{"type":"TimelineAddEntries","entries":[{"entryId":"tweet-1","content":{"itemContent":{"tweet_results":{"result":` + syntheticTweet("1", "1", "") + `}}}}]}]}}}`, ErrNotFound},
		{"thread", "TweetDetail", `{"data":{"threaded_conversation_with_injections_v2":{"instructions":[{"type":"TimelineAddEntries","entries":[{"entryId":"tweet-` + fixtureFocalID + `","content":{"itemContent":{"tweet_results":{"result":{"__typename":"TweetTombstone"}}}}}]}]}}}`, ErrNotFound},
		{"profile", "UserByScreenName", `{"data":{}}`, ErrNotFound},
		{"profile", "UserByScreenName", `{"data":{"user":{}}}`, ErrNotFound},
		{"profile", "UserByScreenName", `{"data":{"user":{"result":{"__typename":"User","legacy":{"screen_name":"u24"}}}}}`, ErrNotFound},
		{"profile", "UserByScreenName", `{"data":{"user":{"result":{"__typename":"UserUnavailable","reason":"Suspended","message":"User is suspended"}}}}`, ErrSuspended},
		{"search", "SearchTimeline", `{"data":{"search_by_raw_query":"not an object"}}`, ErrRequestFailed},
		{"post", "TweetResultByRestId", `{"data":{"tweetResult":{"result":"not an object"}}}`, ErrRequestFailed},
	}
	for _, c := range cases {
		tc := parseCases[c.name]
		if _, err := tc.parse([]byte(c.body)); !errors.Is(err, c.want) {
			t.Errorf("parse %s %s: error = %v, want %v", c.name, c.body, err, c.want)
		}
		if _, err := tc.client(ctx, serveBody(t, c.op, []byte(c.body))); !errors.Is(err, c.want) {
			t.Errorf("client %s %s: error = %v, want %v", c.name, c.body, err, c.want)
		}
	}
}

func TestParseTweetDetailRequiresFocalID(t *testing.T) {
	if _, err := ParseTweetDetail(fixture(t, "tweet_detail_synthetic.json"), ""); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("error = %v, want ErrInvalidParams", err)
	}
}
