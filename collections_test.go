package x

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Synthetic GO2 fixtures. No Live capture exists for the collection reads,
// so every body here is built by hand from the shapes the existing parsers
// already read; the operation paths are the provisional ones in
// collections.go.

func synthUser(id string) string {
	return `{"__typename":"User","rest_id":"` + id + `","core":{"screen_name":"u` + id + `","name":"User ` + id + `"},"legacy":{"followers_count":1}}`
}

// synthList carries fields with no captured path (subscriber_count,
// following, created_at, user_results) to prove they stay unmapped.
func synthList(id string) string {
	return `{"id_str":"` + id + `","name":"list ` + id + `","slug":"list-` + id + `","description":"about ` + id + `",` +
		`"member_count":3,"subscriber_count":9,"mode":"Private","following":true,"created_at":1700000000000,` +
		`"user":{"rest_id":"7","legacy":{"screen_name":"u7","name":"User 7"}},` +
		`"user_results":{"result":{"rest_id":"8"}}}`
}

func itemEntry(id, content string) string {
	return `{"entryId":"item-` + id + `","content":{"entryType":"TimelineTimelineItem","itemContent":` + content + `}}`
}

func tweetItem(id string) string {
	return itemEntry(id, `{"itemType":"TimelineTweet","tweet_results":{"result":`+syntheticTweet(id, id, "")+`}}`)
}

func userItem(id string) string {
	return itemEntry(id, `{"itemType":"TimelineUser","user_results":{"result":`+synthUser(id)+`}}`)
}

func listItem(id string) string {
	return itemEntry(id, `{"itemType":"TimelineTwitterList","list":`+synthList(id)+`}`)
}

// moduleEntry groups item contents in one module entry.
func moduleEntry(id string, contents ...string) string {
	items := make([]string, len(contents))
	for i, c := range contents {
		items[i] = `{"item":{"itemContent":` + c + `}}`
	}
	return `{"entryId":"module-` + id + `","content":{"entryType":"TimelineTimelineModule","items":[` + strings.Join(items, ",") + `]}}`
}

func bottomCursor(v string) string {
	return `{"entryId":"cursor-bottom-0","content":{"entryType":"TimelineTimelineCursor","cursorType":"Bottom","value":"` + v + `"}}`
}

func topCursor(v string) string {
	return `{"entryId":"cursor-top-0","content":{"entryType":"TimelineTimelineCursor","cursorType":"Top","value":"` + v + `"}}`
}

const terminateBottom = `{"type":"TimelineTerminateTimeline","direction":"Bottom"}`

// timelineBody wraps entries in the instruction envelope at a dotted path.
func timelineBody(path string, entries []string, extra ...string) string {
	instructions := append([]string{`{"type":"TimelineAddEntries","entries":[` + strings.Join(entries, ",") + `]}`}, extra...)
	inner := `{"timeline":{"instructions":[` + strings.Join(instructions, ",") + `]}}`
	parts := strings.Split(path, ".")
	for i := len(parts) - 1; i >= 0; i-- {
		inner = `{"` + parts[i] + `":` + inner + `}`
	}
	return `{"data":` + inner + `}`
}

// collectionServer serves body for op and counts the requests it receives.
type collectionServer struct {
	requests atomic.Int64
	vars     atomic.Value
}

func newCollectionClient(t *testing.T, op string, respond func(w http.ResponseWriter)) (*Client, *collectionServer) {
	t.Helper()
	srv := &collectionServer{}
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		srv.requests.Add(1)
		if op != "" && !strings.HasSuffix(r.URL.Path, "/"+op) {
			t.Errorf("request path %s, want operation %s", r.URL.Path, op)
		}
		srv.vars.Store(r.URL.Query().Get("variables"))
		respond(w)
	})
	c.maxRetries = 3
	c.retryBase = time.Millisecond
	if op != "" {
		c.queryIDs[op] = "fixture-" + op
	}
	return c, srv
}

func serveJSON(body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func (s *collectionServer) variables(t *testing.T) map[string]any {
	t.Helper()
	raw, _ := s.vars.Load().(string)
	var vars map[string]any
	if err := json.Unmarshal([]byte(raw), &vars); err != nil {
		t.Fatalf("variables %q: %v", raw, err)
	}
	return vars
}

// collectionCall is one GO2 method under a uniform signature.
type collectionCall struct {
	op       string
	timeline string
	item     func(id string) string
	call     func(ctx context.Context, c *Client, cursor string) (ids []string, next string, hasNext bool, cov PageCoverage, err error)
}

var collectionCalls = map[string]collectionCall{
	"bookmarks": {
		op: "Bookmarks", timeline: "bookmark_timeline_v2.timeline", item: tweetItem,
		call: func(ctx context.Context, c *Client, cursor string) ([]string, string, bool, PageCoverage, error) {
			p, err := c.GetBookmarksPage(ctx, 20, cursor)
			return tweetIDs(p.Tweets), p.NextCursor, p.HasNext, p.Coverage, err
		},
	},
	"liked": {
		op: "Likes", timeline: "user.result.timeline", item: tweetItem,
		call: func(ctx context.Context, c *Client, cursor string) ([]string, string, bool, PageCoverage, error) {
			p, err := c.GetLikedPostsPage(ctx, "42", 20, cursor)
			return tweetIDs(p.Tweets), p.NextCursor, p.HasNext, p.Coverage, err
		},
	},
	"muted": {
		op: "MutedAccounts", timeline: "viewer.muting_timeline", item: userItem,
		call: func(ctx context.Context, c *Client, cursor string) ([]string, string, bool, PageCoverage, error) {
			p, err := c.GetMutedUsersPage(ctx, 20, cursor)
			return userIDs(p.Users), p.NextCursor, p.HasNext, p.Coverage, err
		},
	},
	"blocked": {
		op: "BlockedAccountsAll", timeline: "viewer.timeline", item: userItem,
		call: func(ctx context.Context, c *Client, cursor string) ([]string, string, bool, PageCoverage, error) {
			p, err := c.GetBlockedUsersPage(ctx, 20, cursor)
			return userIDs(p.Users), p.NextCursor, p.HasNext, p.Coverage, err
		},
	},
	"owned_lists": {
		op: "ListOwnerships", timeline: "user.result.timeline", item: listItem,
		call: func(ctx context.Context, c *Client, cursor string) ([]string, string, bool, PageCoverage, error) {
			p, err := c.GetOwnedListsPage(ctx, "42", 20, cursor)
			return listIDs(p.Lists), p.NextCursor, p.HasNext, p.Coverage, err
		},
	},
	"subscribed_lists": {
		op: "ListSubscriptions", timeline: "viewer.list_subscriptions_timeline", item: listItem,
		call: func(ctx context.Context, c *Client, cursor string) ([]string, string, bool, PageCoverage, error) {
			p, err := c.GetSubscribedListsPage(ctx, 20, cursor)
			return listIDs(p.Lists), p.NextCursor, p.HasNext, p.Coverage, err
		},
	},
}

func tweetIDs(ts []Tweet) []string {
	out := []string{}
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

func userIDs(us []User) []string {
	out := []string{}
	for _, u := range us {
		out = append(out, u.ID)
	}
	return out
}

func listIDs(ls []List) []string {
	out := []string{}
	for _, l := range ls {
		out = append(out, l.ID)
	}
	return out
}

// TestListCoverageStallEmptyBudget pins PageEnd for the list reads (and the
// other collection reads, which share the path): stalled, empty, a missing
// continuation, the terminal instruction, drops, partial errors, modules,
// and a budgeted walk that never claims an end it did not see.
func TestListCoverageStallEmptyBudget(t *testing.T) {
	ctx := context.Background()
	for name, tc := range collectionCalls {
		t.Run(name, func(t *testing.T) {
			run := func(t *testing.T, body, cursor string) ([]string, string, bool, PageCoverage, *collectionServer) {
				t.Helper()
				c, srv := newCollectionClient(t, tc.op, serveJSON(body))
				ids, next, hasNext, cov, err := tc.call(ctx, c, cursor)
				if err != nil {
					t.Fatalf("call: %v", err)
				}
				if got := srv.requests.Load(); got != 1 {
					t.Fatalf("requests %d, want 1", got)
				}
				return ids, next, hasNext, cov, srv
			}

			t.Run("more", func(t *testing.T) {
				ids, next, hasNext, cov, _ := run(t, timelineBody(tc.timeline, []string{topCursor("t"), tc.item("1001"), tc.item("1002"), bottomCursor("c2")}), "c1")
				if strings.Join(ids, ",") != "1001,1002" || next != "c2" || !hasNext || cov != (PageCoverage{End: PageEndMore}) {
					t.Fatalf("ids %v next %q hasNext %v coverage %+v", ids, next, hasNext, cov)
				}
			})
			t.Run("stalled", func(t *testing.T) {
				ids, next, hasNext, cov, _ := run(t, timelineBody(tc.timeline, []string{tc.item("1001"), bottomCursor("c1")}), "c1")
				// HasNext keeps its meaning: any Bottom cursor.
				if len(ids) != 1 || next != "c1" || !hasNext || cov.End != PageEndStalled {
					t.Fatalf("ids %v next %q hasNext %v coverage %+v", ids, next, hasNext, cov)
				}
			})
			t.Run("stalled_empty", func(t *testing.T) {
				_, _, hasNext, cov, _ := run(t, timelineBody(tc.timeline, []string{bottomCursor("c1")}), "c1")
				if !hasNext || cov.End != PageEndStalled {
					t.Fatalf("hasNext %v coverage %+v", hasNext, cov)
				}
			})
			t.Run("empty", func(t *testing.T) {
				ids, next, hasNext, cov, _ := run(t, timelineBody(tc.timeline, []string{topCursor("t"), bottomCursor("c2")}), "c1")
				if len(ids) != 0 || next != "c2" || !hasNext || cov.End != PageEndEmpty {
					t.Fatalf("ids %v next %q hasNext %v coverage %+v", ids, next, hasNext, cov)
				}
			})
			t.Run("empty_first_page", func(t *testing.T) {
				_, _, hasNext, cov, _ := run(t, timelineBody(tc.timeline, nil), "")
				if hasNext || cov.End != PageEndEmpty {
					t.Fatalf("hasNext %v coverage %+v", hasNext, cov)
				}
			})
			t.Run("missing_continuation_is_unverified", func(t *testing.T) {
				_, next, hasNext, cov, _ := run(t, timelineBody(tc.timeline, []string{tc.item("1001")}, terminateBottom), "c1")
				if next != "" || hasNext || cov.End != PageEndUnverified {
					t.Fatalf("next %q hasNext %v coverage %+v", next, hasNext, cov)
				}
			})
			t.Run("terminal_only_when_proven", func(t *testing.T) {
				terminalProven[tc.op] = true
				t.Cleanup(func() { delete(terminalProven, tc.op) })
				_, _, _, cov, _ := run(t, timelineBody(tc.timeline, []string{tc.item("1001")}, terminateBottom), "c1")
				if cov.End != PageEndVerified {
					t.Fatalf("proven terminal: %+v", cov)
				}
				_, _, _, cov, _ = run(t, timelineBody(tc.timeline, []string{tc.item("1001")}), "c1")
				if cov.End != PageEndUnverified {
					t.Fatalf("no terminal instruction: %+v", cov)
				}
				_, _, _, cov, _ = run(t, timelineBody(tc.timeline, []string{tc.item("1001"), bottomCursor("c2")}, terminateBottom), "c1")
				if cov.End != PageEndMore {
					t.Fatalf("terminal beside a cursor: %+v", cov)
				}
			})
			t.Run("dropped", func(t *testing.T) {
				var unreadable string
				switch {
				case strings.Contains(tc.item("1"), "TimelineTwitterList"):
					unreadable = itemEntry("9", `{"itemType":"TimelineTwitterList","list":{"name":"no id"}}`)
				case strings.Contains(tc.item("1"), "tweet_results"):
					unreadable = itemEntry("9", `{"itemType":"TimelineTweet","tweet_results":{"result":{"__typename":"TweetTombstone"}}}`)
				case strings.Contains(tc.item("1"), "user_results"):
					unreadable = itemEntry("9", `{"itemType":"TimelineUser","user_results":{"result":{"__typename":"UserUnavailable"}}}`)
				}
				ids, _, _, cov, _ := run(t, timelineBody(tc.timeline, []string{tc.item("1001"), unreadable, bottomCursor("c2")}), "")
				if len(ids) != 1 || cov != (PageCoverage{End: PageEndMore, Dropped: 1}) {
					t.Fatalf("ids %v coverage %+v", ids, cov)
				}
				// A page whose only item was dropped is not empty.
				_, _, _, cov, _ = run(t, timelineBody(tc.timeline, []string{unreadable}), "")
				if cov != (PageCoverage{End: PageEndUnverified, Dropped: 1}) {
					t.Fatalf("dropped-only page: %+v", cov)
				}
			})
			t.Run("partial_errors", func(t *testing.T) {
				body := withErrors(t, []byte(timelineBody(tc.timeline, []string{tc.item("1001"), bottomCursor("c2")})),
					`[{"message":"unavailable","kind":"NonFatal","code":144},{"message":"nested","path":["a","b"]}]`)
				ids, _, _, cov, _ := run(t, string(body), "")
				if len(ids) != 1 || cov != (PageCoverage{End: PageEndMore, PartialErrors: 2}) {
					t.Fatalf("ids %v coverage %+v", ids, cov)
				}
			})
			t.Run("module", func(t *testing.T) {
				inner := func(id string) string {
					entry := tc.item(id)
					start := strings.Index(entry, `"itemContent":`) + len(`"itemContent":`)
					return entry[start : len(entry)-2]
				}
				ids, _, _, _, _ := run(t, timelineBody(tc.timeline, []string{tc.item("1001"), moduleEntry("m", inner("1002"), inner("1003")), bottomCursor("c2")}), "")
				if strings.Join(ids, ",") != "1001,1002,1003" {
					t.Fatalf("module items: %v", ids)
				}
			})
			t.Run("budget", func(t *testing.T) {
				// The provider always hands out a fresh cursor. A walk bounded
				// by a page budget spends one request per page and every page
				// still says more: running out of budget is never an end.
				var page atomic.Int64
				c, srv := newCollectionClient(t, tc.op, func(w http.ResponseWriter) {
					n := page.Add(1)
					id := 1000 + n
					serveJSON(timelineBody(tc.timeline, []string{tc.item(itoa(id)), bottomCursor("c" + itoa(n))}))(w)
				})
				const budget = 3
				cursor := ""
				for i := 0; i < budget; i++ {
					_, next, hasNext, cov, err := tc.call(ctx, c, cursor)
					if err != nil || !hasNext || cov.End != PageEndMore || next == cursor {
						t.Fatalf("page %d: next %q hasNext %v coverage %+v err %v", i, next, hasNext, cov, err)
					}
					cursor = next
				}
				if got := srv.requests.Load(); got != budget {
					t.Fatalf("requests %d, want %d", got, budget)
				}
				if got := srv.variables(t)["cursor"]; got != "c2" {
					t.Fatalf("last request cursor %v, want c2", got)
				}
			})
		})
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// TestListPageMapsOnlyCapturedFields: list pages and GetList map only the
// paths the ListBySlug decoding already reads; the rest wait for Live.
func TestListPageMapsOnlyCapturedFields(t *testing.T) {
	ctx := context.Background()
	c, _ := newCollectionClient(t, "ListOwnerships", serveJSON(timelineBody("user.result.timeline", []string{listItem("1001")})))
	page, err := c.GetOwnedListsPage(ctx, "42", 20, "")
	if err != nil || len(page.Lists) != 1 {
		t.Fatalf("lists %v err %v", page.Lists, err)
	}
	want := List{ID: "1001", Name: "list 1001", Slug: "list-1001", Description: "about 1001", MemberCount: 3,
		OwnerID: "7", OwnerName: "User 7", OwnerScreenName: "u7", IsPrivate: true}
	if got := page.Lists[0]; got.ViewerSubscribed != nil || got.CreatedAt != (time.Time{}) || got.SubscriberCount != 0 ||
		got.ID != want.ID || got.Name != want.Name || got.Slug != want.Slug || got.Description != want.Description ||
		got.MemberCount != want.MemberCount || got.OwnerID != want.OwnerID || got.OwnerName != want.OwnerName ||
		got.OwnerScreenName != want.OwnerScreenName || got.IsPrivate != want.IsPrivate {
		t.Fatalf("list %+v, want %+v", got, want)
	}

	// GetList keeps sending listId to ListBySlug and gains only the owner's
	// screen name.
	c, srv := newCollectionClient(t, "ListBySlug", serveJSON(`{"data":{"list":`+synthList("1001")+`}}`))
	got, err := c.GetList(ctx, "1001")
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerScreenName != "u7" || got.ViewerSubscribed != nil || !got.CreatedAt.IsZero() || got.SubscriberCount != 0 || got.OwnerID != "7" {
		t.Fatalf("GetList %+v", got)
	}
	if vars := srv.variables(t); len(vars) != 1 || vars["listId"] != "1001" {
		t.Fatalf("ListBySlug variables %v", vars)
	}
	raw, _ := json.Marshal(got)
	if want := `{"id":"1001","name":"list 1001","slug":"list-1001","description":"about 1001","memberCount":3,"ownerId":"7","ownerName":"User 7","isPrivate":true,"ownerScreenName":"u7"}`; string(raw) != want {
		t.Fatalf("GetList JSON\n got %s\nwant %s", raw, want)
	}
}

// TestCollectionMethodsRequestShape pins the variables each method sends.
func TestCollectionMethodsRequestShape(t *testing.T) {
	ctx := context.Background()
	for name, tc := range collectionCalls {
		t.Run(name, func(t *testing.T) {
			c, srv := newCollectionClient(t, tc.op, serveJSON(timelineBody(tc.timeline, nil)))
			if _, _, _, _, err := tc.call(ctx, c, "c9"); err != nil {
				t.Fatal(err)
			}
			vars := srv.variables(t)
			if vars["count"] != float64(20) || vars["cursor"] != "c9" || vars["includePromotedContent"] != false {
				t.Fatalf("variables %v", vars)
			}
			_, hasUser := vars["userId"]
			if wantUser := name == "liked" || name == "owned_lists"; hasUser != wantUser || (wantUser && vars["userId"] != "42") {
				t.Fatalf("userId in %v", vars)
			}
			c, srv = newCollectionClient(t, tc.op, serveJSON(timelineBody(tc.timeline, nil)))
			if _, _, _, _, err := tc.call(ctx, c, ""); err != nil {
				t.Fatal(err)
			}
			if _, ok := srv.variables(t)["cursor"]; ok {
				t.Fatal("first page sent a cursor")
			}
		})
	}
	c, srv := newCollectionClient(t, "", serveJSON(`{}`))
	if _, err := c.GetLikedPostsPage(ctx, "", 20, ""); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("empty userID: %v", err)
	}
	if _, err := c.GetOwnedListsPage(ctx, "", 20, ""); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("empty userID: %v", err)
	}
	if srv.requests.Load() != 0 {
		t.Fatal("invalid input sent a request")
	}
}

// TestCollectionsQueryIDUnavailableMakesNoRequest: no query id ships for the
// collection reads, so each refuses with a typed error and sends nothing.
func TestCollectionsQueryIDUnavailableMakesNoRequest(t *testing.T) {
	for name, tc := range collectionCalls {
		t.Run(name, func(t *testing.T) {
			if _, ok := defaultQueryIDs[tc.op]; ok {
				t.Fatalf("%s has a default query id without a Live capture", tc.op)
			}
			c, srv := newCollectionClient(t, "", serveJSON(timelineBody(tc.timeline, []string{tc.item("1001")})))
			_, _, _, _, err := tc.call(context.Background(), c, "")
			var unavailable *QueryIDUnavailableError
			if !errors.Is(err, ErrQueryIDUnavailable) || !errors.As(err, &unavailable) || unavailable.Operation != tc.op {
				t.Fatalf("error %v", err)
			}
			if errors.Is(err, ErrInvalidParams) || errors.Is(err, ErrQueryIDStale) || errors.Is(err, ErrNotFound) {
				t.Fatalf("query id unavailable matches another sentinel: %v", err)
			}
			if got := srv.requests.Load(); got != 0 {
				t.Fatalf("requests %d, want 0", got)
			}
		})
	}
}

// TestCollectionsStaleQueryID: a query id X no longer knows answers a bare
// 404. The collection reads report ErrQueryIDStale with the operation
// evidence, never ErrNotFound or an empty page, after exactly one request.
func TestCollectionsStaleQueryID(t *testing.T) {
	for name, tc := range collectionCalls {
		t.Run(name, func(t *testing.T) {
			for _, ct := range []string{"", "text/html; charset=utf-8"} {
				c, srv := newCollectionClient(t, tc.op, func(w http.ResponseWriter) {
					if ct != "" {
						w.Header().Set("Content-Type", ct)
					}
					w.WriteHeader(http.StatusNotFound)
				})
				_, _, _, _, err := tc.call(context.Background(), c, "")
				var opErr *OperationError
				if !errors.Is(err, ErrQueryIDStale) || errors.Is(err, ErrNotFound) || !errors.As(err, &opErr) ||
					opErr.Operation != tc.op || opErr.Status != http.StatusNotFound {
					t.Fatalf("content type %q: %v", ct, err)
				}
				if strings.Contains(err.Error(), "fixture-") {
					t.Fatal("error carries the query id")
				}
				if got := srv.requests.Load(); got != 1 {
					t.Fatalf("requests %d, want 1", got)
				}
			}

			// A JSON 404 with X's not-found code keeps its classification.
			c, srv := newCollectionClient(t, tc.op, func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errors":[{"code":34,"message":"Sorry, that page does not exist."}]}`))
			})
			_, _, _, _, err := tc.call(context.Background(), c, "")
			if !errors.Is(err, ErrNotFound) || errors.Is(err, ErrQueryIDStale) || srv.requests.Load() != 1 {
				t.Fatalf("JSON 404: %v", err)
			}
		})
	}
}

// TestCollectionsThrottling: a throttle or an automation flag ends the call
// after one request, with the cooldown recorded and no hidden retry.
func TestCollectionsThrottling(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		check  func(*testing.T, *Client, error)
	}{
		{"http429", http.StatusTooManyRequests, `{"errors":[{"code":88,"message":"Rate limit exceeded"}]}`, wantCooldown},
		{"code88", http.StatusOK, `{"errors":[{"code":88,"message":"Rate limit exceeded"}]}`, wantCooldown},
		{"code226", http.StatusOK, `{"errors":[{"code":226,"message":"This request looks like it might be automated."}]}`,
			func(t *testing.T, _ *Client, err error) {
				if !errors.Is(err, ErrAutomatedRequest) || errors.Is(err, ErrRateLimited) {
					t.Fatalf("226: %v", err)
				}
			}},
	}
	for name, tc := range collectionCalls {
		for _, cc := range cases {
			t.Run(name+"/"+cc.name, func(t *testing.T) {
				c, srv := newCollectionClient(t, tc.op, func(w http.ResponseWriter) {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Retry-After", "20")
					w.WriteHeader(cc.status)
					_, _ = w.Write([]byte(cc.body))
				})
				_, _, _, _, err := tc.call(context.Background(), c, "")
				cc.check(t, c, err)
				if got := srv.requests.Load(); got != 1 {
					t.Fatalf("requests %d, want 1", got)
				}
			})
		}
	}
}

func wantCooldown(t *testing.T, c *Client, err error) {
	t.Helper()
	var limited *RateLimitError
	if !errors.As(err, &limited) || !errors.Is(err, ErrRateLimited) || limited.Wait != 20*time.Second {
		t.Fatalf("throttle: %v", err)
	}
	if rl := c.RateLimit(); rl.RetryAfter != 20*time.Second || rl.Remaining != 0 {
		t.Fatalf("cooldown not recorded: %+v", rl)
	}
}

// TestParseUserPageKeepsModuleUsers: users grouped in module entries were
// dropped by parseUserPage; they are now returned, and unreadable ones are
// counted.
func TestParseUserPageKeepsModuleUsers(t *testing.T) {
	unavailable := `{"itemType":"TimelineUser","user_results":{"result":{"__typename":"UserUnavailable"}}}`
	userContent := func(id string) string {
		return `{"itemType":"TimelineUser","user_results":{"result":` + synthUser(id) + `}}`
	}
	body := timelineBody("user.result.timeline", []string{
		userItem("1001"),
		moduleEntry("m", userContent("1002"), unavailable, userContent("1003")),
		bottomCursor("c2"),
	})
	c := serveBody(t, "Followers", []byte(body))
	page, err := c.GetFollowersPage(context.Background(), "42", 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(userIDs(page.Users), ","); got != "1001,1002,1003" {
		t.Fatalf("users %s", got)
	}
	if page.Users[1].ScreenName != "u1002" || page.Coverage != (PageCoverage{End: PageEndMore, Dropped: 1}) || !page.HasNext {
		t.Fatalf("page %+v", page)
	}
	direct, err := parseUserPage(json.RawMessage(mustData(t, body)), "user.result.timeline")
	if err != nil || len(direct.Users) != 3 {
		t.Fatalf("parseUserPage: %v %v", direct.Users, err)
	}
}

func mustData(t *testing.T, body string) string {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatal(err)
	}
	return string(envelope.Data)
}

// TestExistingPageMethodsReportCoverage: the existing session reads the X
// API builds on (home and following feeds, followers, following, list
// timeline and members) report coverage, while HasNext and NextCursor keep
// their meaning and they still retry as before.
func TestExistingPageMethodsReportCoverage(t *testing.T) {
	type call func(ctx context.Context, c *Client, cursor string) (int, string, bool, PageCoverage, error)
	tweets := func(f func(context.Context, *Client, string) (TweetPage, error)) call {
		return func(ctx context.Context, c *Client, cursor string) (int, string, bool, PageCoverage, error) {
			p, err := f(ctx, c, cursor)
			return len(p.Tweets), p.NextCursor, p.HasNext, p.Coverage, err
		}
	}
	users := func(f func(context.Context, *Client, string) (UserPage, error)) call {
		return func(ctx context.Context, c *Client, cursor string) (int, string, bool, PageCoverage, error) {
			p, err := f(ctx, c, cursor)
			return len(p.Users), p.NextCursor, p.HasNext, p.Coverage, err
		}
	}
	methods := []struct {
		op, path string
		item     func(string) string
		call     call
	}{
		{"HomeTimeline", "home.home_timeline_urt", tweetItem, tweets(func(ctx context.Context, c *Client, cur string) (TweetPage, error) {
			return c.HomeTimelinePage(ctx, 20, cur)
		})},
		{"HomeLatestTimeline", "home.home_timeline_urt", tweetItem, tweets(func(ctx context.Context, c *Client, cur string) (TweetPage, error) {
			return c.HomeLatestTimelinePage(ctx, 20, cur)
		})},
		{"ListLatestTweetsTimeline", "list.tweets_timeline", tweetItem, tweets(func(ctx context.Context, c *Client, cur string) (TweetPage, error) {
			return c.GetListTimelinePage(ctx, "1", 20, cur)
		})},
		{"Followers", "user.result.timeline", userItem, users(func(ctx context.Context, c *Client, cur string) (UserPage, error) {
			return c.GetFollowersPage(ctx, "1", 20, cur)
		})},
		{"Following", "user.result.timeline", userItem, users(func(ctx context.Context, c *Client, cur string) (UserPage, error) {
			return c.GetFollowingPage(ctx, "1", 20, cur)
		})},
		{"ListMembers", "list.members_timeline", userItem, users(func(ctx context.Context, c *Client, cur string) (UserPage, error) {
			return c.GetListMembersPage(ctx, "1", 20, cur)
		})},
	}
	ctx := context.Background()
	for _, m := range methods {
		t.Run(m.op, func(t *testing.T) {
			cases := []struct {
				name, body, cursor string
				items              int
				next               string
				hasNext            bool
				want               PageCoverage
			}{
				{"more", timelineBody(m.path, []string{m.item("1001"), bottomCursor("c2")}), "c1", 1, "c2", true, PageCoverage{End: PageEndMore}},
				{"stalled", timelineBody(m.path, []string{m.item("1001"), bottomCursor("c1")}), "c1", 1, "c1", true, PageCoverage{End: PageEndStalled}},
				{"empty", timelineBody(m.path, []string{bottomCursor("c2")}), "c1", 0, "c2", true, PageCoverage{End: PageEndEmpty}},
				{"no_cursor", timelineBody(m.path, []string{m.item("1001")}), "c1", 1, "", false, PageCoverage{End: PageEndUnverified}},
				{"partial", string(withErrors(t, []byte(timelineBody(m.path, []string{m.item("1001"), bottomCursor("c2")})), `[{"message":"x","kind":"NonFatal"}]`)), "", 1, "c2", true, PageCoverage{End: PageEndMore, PartialErrors: 1}},
			}
			for _, tc := range cases {
				c, srv := newCollectionClient(t, m.op, serveJSON(tc.body))
				items, next, hasNext, cov, err := m.call(ctx, c, tc.cursor)
				if err != nil || items != tc.items || next != tc.next || hasNext != tc.hasNext || cov != tc.want || srv.requests.Load() != 1 {
					t.Fatalf("%s: items %d next %q hasNext %v coverage %+v err %v", tc.name, items, next, hasNext, cov, err)
				}
			}
			// Existing reads keep their retry behavior.
			var calls atomic.Int64
			c, _ := newCollectionClient(t, m.op, func(w http.ResponseWriter) {
				if calls.Add(1) == 1 {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				serveJSON(timelineBody(m.path, []string{m.item("1001")}))(w)
			})
			if _, _, _, _, err := m.call(ctx, c, ""); err != nil || calls.Load() != 2 {
				t.Fatalf("retry: calls %d err %v", calls.Load(), err)
			}
		})
	}
}

// TestSearchCoverageFromRealPages: recorded search pages keep their tweets,
// cursor and HasNext, and gain the coverage the parser can decide.
func TestSearchCoverageFromRealPages(t *testing.T) {
	cases := map[string]PageEnd{
		"search_timeline_first.json": PageEndMore,
		"search_timeline_hot.json":   PageEndMore,
		"search_timeline_empty.json": PageEndEmpty,
	}
	for file, want := range cases {
		page, err := ParseSearchTimeline(fixture(t, file))
		if err != nil {
			t.Fatal(err)
		}
		if page.Coverage.End != want || !page.HasNext || page.NextCursor == "" {
			t.Fatalf("%s: %+v hasNext %v", file, page.Coverage, page.HasNext)
		}
	}
}

// TestPageEndValues pins the wire values.
func TestPageEndValues(t *testing.T) {
	got := []PageEnd{PageEndMore, PageEndUnverified, PageEndVerified, PageEndStalled, PageEndEmpty}
	want := []string{"more", "end_unverified", "end_verified", "stalled", "empty"}
	for i := range got {
		if string(got[i]) != want[i] {
			t.Fatalf("PageEnd %d = %q, want %q", i, got[i], want[i])
		}
	}
	if len(terminalProven) != 0 {
		t.Fatalf("terminal instructions proven without a Live capture: %v", terminalProven)
	}
	raw, _ := json.Marshal(TweetPage{Coverage: PageCoverage{End: PageEndMore}})
	if string(raw) != `{"tweets":null,"hasNext":false,"coverage":{"end":"more","dropped":0,"partialErrors":0}}` {
		t.Fatalf("TweetPage JSON %s", raw)
	}
}
