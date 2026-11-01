package x

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Target IDs match the synthetic fixtures.
const (
	typedPostID = "1000000000000000901"
	typedUserID = "1000000000000000903"
)

type typedWrite struct {
	name    string
	gqlOp   string // GraphQL operation, or "" for a form POST
	path    string // REST path for form POSTs
	idVar   string // GraphQL variable or form field carrying the target
	extra   map[string]any
	target  string
	call    func(*Client, context.Context, string) (WriteResult, error)
	follows bool
}

var typedWrites = []typedWrite{
	{name: "LikePost", gqlOp: "FavoriteTweet", idVar: "tweet_id", target: typedPostID, call: (*Client).LikePost},
	{name: "UnlikePost", gqlOp: "UnfavoriteTweet", idVar: "tweet_id", target: typedPostID, call: (*Client).UnlikePost},
	{name: "Repost", gqlOp: "CreateRetweet", idVar: "tweet_id", extra: map[string]any{"dark_request": false}, target: typedPostID, call: (*Client).Repost},
	{name: "Unrepost", gqlOp: "DeleteRetweet", idVar: "source_tweet_id", extra: map[string]any{"dark_request": false}, target: typedPostID, call: (*Client).Unrepost},
	{name: "BookmarkPost", gqlOp: "CreateBookmark", idVar: "tweet_id", target: typedPostID, call: (*Client).BookmarkPost},
	{name: "UnbookmarkPost", gqlOp: "DeleteBookmark", idVar: "tweet_id", target: typedPostID, call: (*Client).UnbookmarkPost},
	{name: "DeletePost", gqlOp: "DeleteTweet", idVar: "tweet_id", extra: map[string]any{"dark_request": false}, target: typedPostID, call: (*Client).DeletePost},
	{name: "FollowUser", path: "/i/api/1.1/friendships/create.json", idVar: "user_id", target: typedUserID, call: (*Client).FollowUser, follows: true},
	{name: "UnfollowUser", path: "/i/api/1.1/friendships/destroy.json", idVar: "user_id", target: typedUserID, call: (*Client).UnfollowUser},
	{name: "MuteUser", path: "/i/api/1.1/mutes/users/create.json", idVar: "user_id", target: typedUserID, call: (*Client).MuteUser},
	{name: "UnmuteUser", path: "/i/api/1.1/mutes/users/destroy.json", idVar: "user_id", target: typedUserID, call: (*Client).UnmuteUser},
	{name: "BlockUser", path: "/i/api/1.1/blocks/create.json", idVar: "user_id", target: typedUserID, call: (*Client).BlockUser},
	{name: "UnblockUser", path: "/i/api/1.1/blocks/destroy.json", idVar: "user_id", target: typedUserID, call: (*Client).UnblockUser},
}

func (w typedWrite) graphql() bool { return w.gqlOp != "" }

type seenRequest struct {
	method      string
	path        string
	contentType string
	body        string
}

// recordingClient serves every request with respond and records it. Retries
// are enabled so a test fails if any request is repeated.
func recordingClient(t *testing.T, respond func(http.ResponseWriter, *http.Request)) (*Client, func() []seenRequest) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []seenRequest
	)
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, seenRequest{r.Method, r.URL.Path, r.Header.Get("Content-Type"), string(body)})
		mu.Unlock()
		respond(w, r)
	})
	c.maxRetries = 3
	c.retryBase = time.Millisecond
	return c, func() []seenRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]seenRequest(nil), seen...)
	}
}

func respondWith(status int, contentType, body string, headers ...string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func restFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "rest", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// outcomeOf asserts err is an *OutcomeError for method and returns it.
func outcomeOf(t *testing.T, method string, res WriteResult, err error) *OutcomeError {
	t.Helper()
	if res != (WriteResult{}) {
		t.Fatalf("result = %+v, want zero on failure", res)
	}
	var oe *OutcomeError
	if !errors.As(err, &oe) {
		t.Fatalf("error %v (%T) is not *OutcomeError", err, err)
	}
	if oe.Operation != method {
		t.Fatalf("Operation = %q, want %q", oe.Operation, method)
	}
	return oe
}

func wantAmbiguous(t *testing.T, oe *OutcomeError) {
	t.Helper()
	if !oe.Ambiguous || oe.NotAttempted || oe.Outcome != ErrAmbiguous || !errors.Is(oe, ErrAmbiguous) || errors.Is(oe, ErrDefinite) {
		t.Fatalf("outcome %+v: want ambiguous only", oe)
	}
}

func wantDefinite(t *testing.T, oe *OutcomeError, notAttempted bool) {
	t.Helper()
	if oe.Ambiguous || oe.NotAttempted != notAttempted || oe.Outcome != ErrDefinite || !errors.Is(oe, ErrDefinite) || errors.Is(oe, ErrAmbiguous) {
		t.Fatalf("outcome %+v: want definite (NotAttempted=%v)", oe, notAttempted)
	}
}

// checkRequest asserts the one request a write sent: method, path, and the
// target in the body exactly as the untyped method sends it.
func checkRequest(t *testing.T, w typedWrite, c *Client, req seenRequest) {
	t.Helper()
	if req.method != http.MethodPost {
		t.Fatalf("method = %s, want POST", req.method)
	}
	if !w.graphql() {
		if req.path != w.path || req.contentType != "application/x-www-form-urlencoded" {
			t.Fatalf("request %s %q, want %s form", req.path, req.contentType, w.path)
		}
		form, err := url.ParseQuery(req.body)
		if err != nil || len(form) != 1 || form.Get("user_id") != w.target {
			t.Fatalf("form = %q, want only user_id=%s", req.body, w.target)
		}
		return
	}
	if want := "/i/api/graphql/" + c.queryIDs[w.gqlOp] + "/" + w.gqlOp; req.path != want {
		t.Fatalf("path = %s, want %s", req.path, want)
	}
	var payload struct {
		Variables map[string]any `json:"variables"`
		QueryID   string         `json:"queryId"`
	}
	if err := json.Unmarshal([]byte(req.body), &payload); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{w.idVar: w.target}
	for k, v := range w.extra {
		want[k] = v
	}
	if len(payload.Variables) != len(want) || payload.QueryID != c.queryIDs[w.gqlOp] {
		t.Fatalf("payload %+v, want variables %v", payload, want)
	}
	for k, v := range want {
		if payload.Variables[k] != v {
			t.Fatalf("variables %v, want %v", payload.Variables, want)
		}
	}
}

// TestWritesSendOnceNoRedirect: every typed write sends exactly one request,
// refuses a redirect as ambiguous without following it, never retries a
// server error, and leaves the shared client used by the untyped methods
// unchanged.
func TestWritesSendOnceNoRedirect(t *testing.T) {
	ctx := context.Background()
	for _, w := range typedWrites {
		t.Run(w.name+"/redirect", func(t *testing.T) {
			c, seen := recordingClient(t, func(rw http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/elsewhere" {
					rw.WriteHeader(http.StatusOK)
					return
				}
				http.Redirect(rw, r, "/elsewhere", http.StatusFound)
			})
			shared := c.httpClient
			res, err := w.call(c, ctx, w.target)
			oe := outcomeOf(t, w.name, res, err)
			wantAmbiguous(t, oe)
			if oe.Status != http.StatusFound || !errors.Is(err, errRedirectRefused) {
				t.Fatalf("error %v status %d, want refused 302", err, oe.Status)
			}
			reqs := seen()
			if len(reqs) != 1 {
				t.Fatalf("requests = %+v, want exactly 1 (redirect not followed)", reqs)
			}
			checkRequest(t, w, c, reqs[0])
			if c.httpClient != shared || c.httpClient.CheckRedirect != nil {
				t.Fatal("shared http.Client was modified")
			}
		})
		t.Run(w.name+"/server_error", func(t *testing.T) {
			c, seen := recordingClient(t, respondWith(http.StatusServiceUnavailable, "text/plain", "upstream"))
			res, err := w.call(c, ctx, w.target)
			oe := outcomeOf(t, w.name, res, err)
			wantAmbiguous(t, oe)
			if oe.Status != http.StatusServiceUnavailable {
				t.Fatalf("status = %d", oe.Status)
			}
			if n := len(seen()); n != 1 {
				t.Fatalf("requests = %d, want 1 (never retried)", n)
			}
		})
		t.Run(w.name+"/server_error_known_code", func(t *testing.T) {
			// Spec 8.5: a 5xx is uncertain; the 226 sentinel stays in Err so
			// a breaker can still trip.
			c, seen := recordingClient(t, respondWith(http.StatusServiceUnavailable, "application/json", `{"errors":[{"code":226,"message":"This request looks like it might be automated."}]}`))
			res, err := w.call(c, ctx, w.target)
			oe := outcomeOf(t, w.name, res, err)
			wantAmbiguous(t, oe)
			if oe.Status != http.StatusServiceUnavailable || oe.Code != 226 || !errors.Is(err, ErrAutomatedRequest) {
				t.Fatalf("error %v %+v, want ambiguous 503 carrying ErrAutomatedRequest and code 226", err, oe)
			}
			if n := len(seen()); n != 1 {
				t.Fatalf("requests = %d, want 1 (never retried)", n)
			}
		})
		t.Run(w.name+"/transport_reset", func(t *testing.T) {
			var received atomic.Int32
			c := newServerClient(t, func(rw http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				received.Add(1)
				conn, _, err := rw.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
			})
			c.maxRetries = 3
			res, err := w.call(c, ctx, w.target)
			oe := outcomeOf(t, w.name, res, err)
			wantAmbiguous(t, oe)
			if oe.Status != 0 || received.Load() != 1 {
				t.Fatalf("status %d requests %d, want 0 and 1", oe.Status, received.Load())
			}
		})
	}
}

// TestUntypedWritesKeepSharedClient pins the existing methods: Like still
// goes through the shared client (which follows redirects) and Follow still
// returns a bare context error on a pre-send cancel.
func TestUntypedWritesKeepSharedClient(t *testing.T) {
	var hits atomic.Int32
	c := newServerClient(t, func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/elsewhere" {
			rw.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(rw, `{"data":{"favorite_tweet":"Done"}}`)
			return
		}
		http.Redirect(rw, r, "/elsewhere", http.StatusFound)
	})
	if err := c.Like(context.Background(), typedPostID); err != nil {
		t.Fatalf("Like: %v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("Like requests = %d, want 2 (shared client follows redirects as before)", hits.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Follow(ctx, typedUserID); err != context.Canceled {
		t.Fatalf("Follow on a cancelled context = %v, want bare context.Canceled", err)
	}
}

// TestEmptyDataAckAmbiguous: a GraphQL write is applied only with no errors
// and a registered ack root holding a value. No ack root has been captured,
// so every shipped GraphQL write answers a clean 200 as ambiguous, and
// {"data":{}} is ambiguous even with a root registered. Form POSTs are
// applied only on a 200 JSON user whose id_str is the target.
func TestEmptyDataAckAmbiguous(t *testing.T) {
	ctx := context.Background()
	gqlBodies := []string{
		`{"data":{}}`,
		`{"data":null}`,
		`{}`,
		`{"data":{"favorite_tweet":"Done"}}`,
		`{"data":{"create_retweet":{"retweet_results":{"result":{"rest_id":"1000000000000000904"}}}}}`,
		`not json`,
	}
	formBodies := []struct{ contentType, body string }{
		{"application/json", `{}`},
		{"application/json", `{"id_str":"1000000000000000905"}`},
		{"application/json", `{"id_str":""}`},
		{"application/json", `[]`},
		{"application/json", `not json`},
		{"text/html", restFixture(t, "user_synthetic.json")},
		{"", restFixture(t, "user_synthetic.json")},
	}
	for _, w := range typedWrites {
		if w.graphql() {
			for _, body := range gqlBodies {
				c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json", body))
				res, err := w.call(c, ctx, w.target)
				oe := outcomeOf(t, w.name, res, err)
				wantAmbiguous(t, oe)
				if oe.Status != http.StatusOK || len(seen()) != 1 {
					t.Fatalf("%s %s: status %d requests %d", w.name, body, oe.Status, len(seen()))
				}
			}
			continue
		}
		for _, fb := range formBodies {
			c, seen := recordingClient(t, respondWith(http.StatusOK, fb.contentType, fb.body))
			res, err := w.call(c, ctx, w.target)
			oe := outcomeOf(t, w.name, res, err)
			wantAmbiguous(t, oe)
			if len(seen()) != 1 {
				t.Fatalf("%s: requests %d", w.name, len(seen()))
			}
		}
	}

	// The ack rule itself, with a root registered as a Live capture would.
	op := likePostOp
	op.ackRoot = "favorite_tweet"
	cases := []struct {
		body    string
		applied bool
	}{
		{`{"data":{"favorite_tweet":"Done"}}`, true},
		{`{"data":{}}`, false},
		{`{"data":{"favorite_tweet":null}}`, false},
		{`{"data":{"favorite_tweet":{}}}`, false},
		{`{"data":{"other":"Done"}}`, false},
		{`{"data":{"favorite_tweet":"Done"},"errors":[{"message":"x","code":139}]}`, false},
	}
	for _, tc := range cases {
		c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json", tc.body))
		res, err := c.graphqlWrite(ctx, op, typedPostID)
		if tc.applied {
			if err != nil || res != (WriteResult{Operation: "LikePost", TargetID: typedPostID, State: WriteApplied}) {
				t.Fatalf("%s: %+v %v, want applied", tc.body, res, err)
			}
		} else {
			wantAmbiguous(t, outcomeOf(t, "LikePost", res, err))
		}
		if len(seen()) != 1 {
			t.Fatalf("%s: requests %d", tc.body, len(seen()))
		}
	}
	for name, root := range map[string]string{
		likePostOp.method: likePostOp.ackRoot, unlikePostOp.method: unlikePostOp.ackRoot,
		repostOp.method: repostOp.ackRoot, unrepostOp.method: unrepostOp.ackRoot,
		bookmarkPostOp.method: bookmarkPostOp.ackRoot, unbookmarkPostOp.method: unbookmarkPostOp.ackRoot,
		deletePostOp.method: deletePostOp.ackRoot,
	} {
		if root != "" {
			t.Errorf("%s has ack root %q; roots come only from recorded captures", name, root)
		}
	}
}

func TestFormWritesApplyOnlyOnMatchingUser(t *testing.T) {
	ctx := context.Background()
	for _, w := range typedWrites {
		if w.graphql() {
			continue
		}
		c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json; charset=utf-8", restFixture(t, "user_synthetic.json")))
		res, err := w.call(c, ctx, w.target)
		if err != nil || res != (WriteResult{Operation: w.name, TargetID: w.target, State: WriteApplied}) {
			t.Fatalf("%s: %+v %v, want applied", w.name, res, err)
		}
		reqs := seen()
		if len(reqs) != 1 {
			t.Fatalf("%s: requests %d", w.name, len(reqs))
		}
		checkRequest(t, w, c, reqs[0])

		c, _ = recordingClient(t, respondWith(http.StatusOK, "application/json", restFixture(t, "user_follow_request_sent_synthetic.json")))
		res, err = w.call(c, ctx, w.target)
		want := WriteApplied
		if w.follows {
			want = WriteRequested
		}
		if err != nil || res.State != want {
			t.Fatalf("%s with follow_request_sent: %+v %v, want %s", w.name, res, err, want)
		}
	}
}

// TestWriteStaleQueryID: a missing query id sends nothing; a 404 without an
// X error body is ambiguous (spec 8.5: uncertain delivery_unknown) and wraps
// ErrQueryIDStale, never ErrNotFound; X's own not-found codes 34/144 stay
// ErrNotFound with the code recorded; a 404 with any other unmapped code is
// ambiguous and never ErrNotFound.
func TestWriteStaleQueryID(t *testing.T) {
	ctx := context.Background()
	for _, w := range typedWrites {
		if !w.graphql() {
			continue
		}
		t.Run(w.name+"/unavailable", func(t *testing.T) {
			c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json", `{"data":{}}`))
			delete(c.queryIDs, w.gqlOp)
			res, err := w.call(c, ctx, w.target)
			oe := outcomeOf(t, w.name, res, err)
			wantDefinite(t, oe, true)
			if !errors.Is(err, ErrQueryIDUnavailable) || len(seen()) != 0 {
				t.Fatalf("error %v requests %d, want ErrQueryIDUnavailable and none", err, len(seen()))
			}
		})
		t.Run(w.name+"/stale", func(t *testing.T) {
			for _, resp := range []struct{ contentType, body string }{
				{"", ""},
				{"text/html", "<html><body>Not Found</body></html>"},
			} {
				c, seen := recordingClient(t, respondWith(http.StatusNotFound, resp.contentType, resp.body))
				res, err := w.call(c, ctx, w.target)
				oe := outcomeOf(t, w.name, res, err)
				wantAmbiguous(t, oe)
				if !errors.Is(err, ErrQueryIDStale) || errors.Is(err, ErrNotFound) || oe.Status != 404 || oe.Code != 0 {
					t.Fatalf("%q: error %v %+v, want ErrQueryIDStale only", resp.body, err, oe)
				}
				if c.QueryMetadataRefreshed() || len(seen()) != 1 {
					t.Fatalf("%q: requests %d refreshed %v, want 1 and no refresh", resp.body, len(seen()), c.QueryMetadataRefreshed())
				}
			}
		})
		t.Run(w.name+"/not_found_code", func(t *testing.T) {
			c, _ := recordingClient(t, respondWith(http.StatusNotFound, "application/json", `{"errors":[{"code":144,"message":"No status found with that ID."}]}`))
			res, err := w.call(c, ctx, w.target)
			oe := outcomeOf(t, w.name, res, err)
			wantDefinite(t, oe, false)
			if !errors.Is(err, ErrNotFound) || oe.Code != 144 || oe.Status != 404 {
				t.Fatalf("error %v %+v, want ErrNotFound code 144", err, oe)
			}
		})
	}
	// Only codes 34 and 144 prove an absent target: a 404 with any other
	// unmapped code, even with a "not found" message, is ambiguous.
	for _, w := range typedWrites {
		for _, body := range []string{
			`{"errors":[{"code":9999,"message":"something"}]}`,
			`{"errors":[{"code":9998,"message":"Page not found"}]}`,
		} {
			c, seen := recordingClient(t, respondWith(http.StatusNotFound, "application/json", body))
			res, err := w.call(c, ctx, w.target)
			oe := outcomeOf(t, w.name, res, err)
			wantAmbiguous(t, oe)
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrQueryIDStale) || !errors.Is(err, ErrRequestFailed) || oe.Status != 404 || oe.Code == 0 || len(seen()) != 1 {
				t.Fatalf("%s %s: error %v %+v requests %d, want ErrRequestFailed with the code, ambiguous", w.name, body, err, oe, len(seen()))
			}
		}
	}

	// A form path has no query id: a bare 404 proves nothing and is ambiguous.
	c, _ := recordingClient(t, respondWith(http.StatusNotFound, "", ""))
	res, err := c.FollowUser(ctx, typedUserID)
	oe := outcomeOf(t, "FollowUser", res, err)
	wantAmbiguous(t, oe)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrQueryIDStale) || oe.Status != 404 {
		t.Fatalf("FollowUser bare 404: %v %+v", err, oe)
	}

	// The state reads use the same rules.
	reads := map[string]func(*Client) error{
		"TweetResultByRestId": func(c *Client) error { _, err := c.GetPostState(ctx, typedPostID); return err },
		"UserByRestId":        func(c *Client) error { _, err := c.GetUserRelationship(ctx, typedUserID); return err },
	}
	for op, read := range reads {
		c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json", `{"data":{}}`))
		delete(c.queryIDs, op)
		if err := read(c); !errors.Is(err, ErrQueryIDUnavailable) || len(seen()) != 0 {
			t.Fatalf("%s without query id: %v, %d requests", op, err, len(seen()))
		}
		c, seen = recordingClient(t, respondWith(http.StatusNotFound, "", ""))
		if err := read(c); !errors.Is(err, ErrQueryIDStale) || errors.Is(err, ErrNotFound) || len(seen()) != 1 {
			t.Fatalf("%s bare 404: %v, %d requests", op, err, len(seen()))
		}
		c, _ = recordingClient(t, respondWith(http.StatusNotFound, "application/json", `{"errors":[{"code":9999,"message":"not found"}]}`))
		if err := read(c); !errors.Is(err, ErrRequestFailed) || errors.Is(err, ErrNotFound) {
			t.Fatalf("%s 404 with unmapped code: %v, want ErrRequestFailed and not ErrNotFound", op, err)
		}
		c, _ = recordingClient(t, respondWith(http.StatusNotFound, "application/json", `{"errors":[{"code":34,"message":"Sorry, that page does not exist."}]}`))
		if err := read(c); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s 404 with code 34: %v, want ErrNotFound", op, err)
		}
	}
}

// TestWriteThrottling: HTTP 429 and X code 88 are definite rate limits with
// the provider cooldown recorded, sent once and never retried.
func TestWriteThrottling(t *testing.T) {
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
		{"code_88_403", http.StatusForbidden, `{"errors":[{"code":88,"message":"Rate limit exceeded"}]}`, 88},
	}
	for _, w := range typedWrites {
		for _, resp := range responses {
			t.Run(w.name+"/"+resp.name, func(t *testing.T) {
				c, seen := recordingClient(t, respondWith(resp.status, "application/json", resp.body, "Retry-After", "120"))
				res, err := w.call(c, ctx, w.target)
				oe := outcomeOf(t, w.name, res, err)
				wantDefinite(t, oe, false)
				var rle *RateLimitError
				if !errors.Is(err, ErrRateLimited) || !errors.As(err, &rle) || rle.Wait != 120*time.Second {
					t.Fatalf("error %v, want *RateLimitError with 120s", err)
				}
				if oe.Status != resp.status || oe.Code != resp.wantCode {
					t.Fatalf("status %d code %d, want %d %d", oe.Status, oe.Code, resp.status, resp.wantCode)
				}
				if !c.RateLimit().IsLimited() {
					t.Fatal("cooldown not recorded")
				}
				if n := len(seen()); n != 1 {
					t.Fatalf("requests = %d, want 1", n)
				}
			})
		}
	}
	for _, resp := range responses[:3] {
		c, seen := recordingClient(t, respondWith(resp.status, "application/json", resp.body, "Retry-After", "120"))
		_, err := c.GetPostState(ctx, typedPostID)
		var rle *RateLimitError
		if !errors.As(err, &rle) || len(seen()) != 1 || !c.RateLimit().IsLimited() {
			t.Fatalf("GetPostState %s: %v, %d requests", resp.name, err, len(seen()))
		}
	}
}

func TestWriteNotAttempted(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, w := range typedWrites {
		c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json", `{}`))
		res, err := w.call(c, cancelled, w.target)
		oe := outcomeOf(t, w.name, res, err)
		wantDefinite(t, oe, true)
		if !errors.Is(err, context.Canceled) || !errors.Is(err, errWriteNotAttempted) || len(seen()) != 0 {
			t.Fatalf("%s cancelled: %v, %d requests", w.name, err, len(seen()))
		}
		for _, id := range []string{"", "abc", "0", "012", "1e5", " 1", "123456789012345678901"} {
			res, err := w.call(c, context.Background(), id)
			oe := outcomeOf(t, w.name, res, err)
			wantDefinite(t, oe, true)
			if !errors.Is(err, ErrInvalidParams) {
				t.Fatalf("%s(%q): %v", w.name, id, err)
			}
		}
		if n := len(seen()); n != 0 {
			t.Fatalf("%s: %d requests for invalid input", w.name, n)
		}
	}

	// A cancel while waiting for the request gap is also not attempted.
	for _, w := range typedWrites {
		c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json", `{}`))
		c.minGap = time.Hour
		c.lastReqAt = time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		res, err := w.call(c, ctx, w.target)
		cancel()
		wantDefinite(t, outcomeOf(t, w.name, res, err), true)
		if !errors.Is(err, context.DeadlineExceeded) || len(seen()) != 0 {
			t.Fatalf("%s gap cancel: %v, %d requests", w.name, err, len(seen()))
		}
	}
}

// TestWriteRejectionsKeepClassification: known X codes are definite with the
// code recorded; 327 stays ErrAlreadyRetweeted and ambiguous, never
// already_in_state; unknown codes and errors next to data are ambiguous.
func TestWriteRejectionsKeepClassification(t *testing.T) {
	ctx := context.Background()
	for _, w := range typedWrites {
		for _, status := range []int{http.StatusOK, http.StatusForbidden} {
			c, _ := recordingClient(t, respondWith(status, "application/json", `{"errors":[{"code":226,"message":"automated"}]}`))
			res, err := w.call(c, ctx, w.target)
			oe := outcomeOf(t, w.name, res, err)
			wantDefinite(t, oe, false)
			if !errors.Is(err, ErrAutomatedRequest) || oe.Code != 226 || oe.Status != status {
				t.Fatalf("%s 226/%d: %v %+v", w.name, status, err, oe)
			}

			c, _ = recordingClient(t, respondWith(status, "application/json", `{"errors":[{"code":327,"message":"You have already retweeted this Tweet."}]}`))
			res, err = w.call(c, ctx, w.target)
			oe = outcomeOf(t, w.name, res, err)
			wantAmbiguous(t, oe)
			if !errors.Is(err, ErrAlreadyRetweeted) || oe.Code != 327 {
				t.Fatalf("%s 327/%d: %v %+v", w.name, status, err, oe)
			}

			// An unknown code is ambiguous on a 200; on a 403 the existing
			// status classification (ErrForbidden, definite) applies.
			c, _ = recordingClient(t, respondWith(status, "application/json", `{"errors":[{"code":9999,"message":"something new"}]}`))
			res, err = w.call(c, ctx, w.target)
			oe = outcomeOf(t, w.name, res, err)
			if status == http.StatusOK {
				wantAmbiguous(t, oe)
			} else if wantDefinite(t, oe, false); !errors.Is(err, ErrForbidden) {
				t.Fatalf("%s unknown code on 403: %v", w.name, err)
			}
			if oe.Code != 9999 {
				t.Fatalf("%s unknown code: %+v", w.name, oe)
			}
		}
		if w.graphql() {
			c, _ := recordingClient(t, respondWith(http.StatusOK, "application/json", `{"data":{"favorite_tweet":"Done"},"errors":[{"code":144,"message":"not found"}]}`))
			res, err := w.call(c, ctx, w.target)
			wantAmbiguous(t, outcomeOf(t, w.name, res, err))
		}
	}
}

func TestOutcomeErrorCompatibility(t *testing.T) {
	inner := errors.New("boom")
	legacy := &OutcomeError{Outcome: ErrDefinite, Err: inner}
	if got, want := legacy.Error(), ErrDefinite.Error()+": boom"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(legacy, ErrDefinite) || !errors.Is(legacy, inner) || errors.Is(legacy, ErrAmbiguous) {
		t.Fatal("legacy OutcomeError matching changed")
	}
	if got := (&OutcomeError{Outcome: ErrAmbiguous, Err: errMissingAck}).Error(); got != ErrAmbiguous.Error()+": "+errMissingAck.Error() {
		t.Fatalf("Error() = %q", got)
	}
	for _, e := range []*OutcomeError{{}, {Err: inner}, {Ambiguous: true}, {NotAttempted: true, Err: inner}} {
		_ = e.Error() // must not panic with a nil Outcome or Err
	}
	if !errors.Is(&OutcomeError{Ambiguous: true, Err: inner}, ErrAmbiguous) || !errors.Is(&OutcomeError{NotAttempted: true}, ErrDefinite) {
		t.Fatal("flags without Outcome should imply the outcome sentinel")
	}
	if got := (&OutcomeError{Err: inner}).Error(); got != "x: write failed: boom" {
		t.Fatalf("Error() = %q", got)
	}
	// classifyWriteOutcome keeps its shape for existing callers.
	var oe *OutcomeError
	if !errors.As(classifyWriteOutcome(ErrRateLimited), &oe) || oe.Outcome != ErrDefinite || oe.Operation != "" || oe.Ambiguous || oe.NotAttempted {
		t.Fatalf("classifyWriteOutcome = %+v", oe)
	}
}

func TestGetPostState(t *testing.T) {
	ctx := context.Background()
	body := fixture(t, "post_state_viewer_synthetic.json")
	c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json", string(body)))
	state, err := c.GetPostState(ctx, typedPostID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Kind != PostPresent || state.Tweet == nil || state.Tweet.ID != typedPostID {
		t.Fatalf("state = %+v", state)
	}
	if v := state.Tweet.Viewer; v == nil || *v != (PostViewerState{Liked: true, Reposted: false, Bookmarked: true}) {
		t.Fatalf("viewer = %+v", state.Tweet.Viewer)
	}
	reqs := seen()
	if len(reqs) != 1 || reqs[0].method != http.MethodGet || !strings.HasSuffix(reqs[0].path, "/TweetResultByRestId") {
		t.Fatalf("requests = %+v", reqs)
	}

	// The Parse functions and GetTweet never set Viewer, and a nil Viewer
	// leaves the Tweet JSON unchanged.
	parsed, err := ParseTweetResultByRestID(body)
	if err != nil || parsed.Viewer != nil {
		t.Fatalf("ParseTweetResultByRestID viewer = %+v, %v", parsed.Viewer, err)
	}
	got, err := serveBody(t, "TweetResultByRestId", body).GetTweet(ctx, typedPostID)
	if err != nil || got.Viewer != nil {
		t.Fatalf("GetTweet viewer = %+v, %v", got.Viewer, err)
	}
	if encoded, _ := json.Marshal(parsed); strings.Contains(string(encoded), `"viewer"`) {
		t.Fatal("nil Viewer appears in Tweet JSON")
	}
	withViewer := *state.Tweet
	withViewer.Raw = nil
	parsed.Raw = nil
	withViewer.Viewer = nil
	if a, b := encodeJSON(t, withViewer), encodeJSON(t, parsed); a != b {
		t.Fatalf("GetPostState tweet differs from the parser beyond Viewer:\n%s\n%s", a, b)
	}

	tweet := func(legacy string) string {
		return `{"data":{"tweetResult":{"result":{"__typename":"Tweet","rest_id":"` + typedPostID + `","legacy":{"full_text":"filler"` + legacy + `}}}}}`
	}
	cases := []struct {
		name   string
		body   string
		want   PostState
		err    error
		viewer bool
	}{
		{"tombstone", `{"data":{"tweetResult":{"result":{"__typename":"TweetTombstone","tombstone":{"text":{"text":"This Post was deleted by the Post author."}}}}}}`, PostState{Kind: PostTombstone, Reason: "This Post was deleted by the Post author."}, nil, false},
		{"unavailable", `{"data":{"tweetResult":{"result":{"__typename":"TweetUnavailable","reason":"Suspended"}}}}`, PostState{Kind: PostUnavailable, Reason: "Suspended"}, nil, false},
		{"empty", `{"data":{"tweetResult":{}}}`, PostState{Kind: PostEmpty}, nil, false},
		{"empty_null", `{"data":{"tweetResult":{"result":null}}}`, PostState{Kind: PostEmpty}, nil, false},
		{"no_root", `{"data":{}}`, PostState{}, ErrRequestFailed, false},
		{"other_post", `{"data":{"tweetResult":{"result":{"__typename":"Tweet","rest_id":"1000000000000000909","legacy":{}}}}}`, PostState{}, ErrRequestFailed, false},
		{"no_id", `{"data":{"tweetResult":{"result":{"__typename":"Tweet","legacy":{}}}}}`, PostState{}, ErrRequestFailed, false},
		{"unknown_type", `{"data":{"tweetResult":{"result":{"__typename":"TweetSomethingNew"}}}}`, PostState{}, ErrRequestFailed, false},
		{"not_found_code", `{"errors":[{"code":144,"message":"No status found with that ID."}]}`, PostState{}, ErrNotFound, false},
		{"partial_flags", tweet(`,"favorited":true,"retweeted":false`), PostState{Kind: PostPresent}, nil, false},
		{"visibility_wrapper", `{"data":{"tweetResult":{"result":{"__typename":"TweetWithVisibilityResults","tweet":{"rest_id":"` + typedPostID + `","legacy":{"favorited":false,"retweeted":true,"bookmarked":false}}}}}}`, PostState{Kind: PostPresent}, nil, true},
	}
	for _, tc := range cases {
		c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json", tc.body))
		state, err := c.GetPostState(ctx, typedPostID)
		if len(seen()) != 1 {
			t.Fatalf("%s: %d requests", tc.name, len(seen()))
		}
		if tc.err != nil {
			if !errors.Is(err, tc.err) {
				t.Fatalf("%s: error %v, want %v", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil || state.Kind != tc.want.Kind || state.Reason != tc.want.Reason {
			t.Fatalf("%s: %+v %v, want %+v", tc.name, state, err, tc.want)
		}
		if tc.want.Kind == PostPresent {
			if (state.Tweet.Viewer != nil) != tc.viewer {
				t.Fatalf("%s: viewer = %+v", tc.name, state.Tweet.Viewer)
			}
			if tc.viewer && *state.Tweet.Viewer != (PostViewerState{Reposted: true}) {
				t.Fatalf("%s: viewer = %+v", tc.name, state.Tweet.Viewer)
			}
		} else if state.Tweet != nil {
			t.Fatalf("%s: tweet set for %s", tc.name, state.Kind)
		}
	}

	// Redirects are refused for reads too.
	c, seen = recordingClient(t, func(rw http.ResponseWriter, r *http.Request) { http.Redirect(rw, r, "/elsewhere", http.StatusFound) })
	if _, err := c.GetPostState(ctx, typedPostID); !errors.Is(err, errRedirectRefused) || len(seen()) != 1 {
		t.Fatalf("redirect: %v, %d requests", err, len(seen()))
	}
	if _, err := c.GetPostState(ctx, "abc"); !errors.Is(err, ErrInvalidParams) || len(seen()) != 1 {
		t.Fatalf("invalid id: %v", err)
	}
}

func encodeJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGetUserRelationship(t *testing.T) {
	ctx := context.Background()
	c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json", string(fixture(t, "user_relationship_synthetic.json"))))
	rel, err := c.GetUserRelationship(ctx, typedUserID)
	if err != nil {
		t.Fatal(err)
	}
	want := Relationship{FollowedBy: true, FollowRequestSent: true, Muting: true}
	if rel != want {
		t.Fatalf("relationship = %+v, want %+v", rel, want)
	}
	reqs := seen()
	if len(reqs) != 1 || reqs[0].method != http.MethodGet || !strings.HasSuffix(reqs[0].path, "/UserByRestId") {
		t.Fatalf("requests = %+v", reqs)
	}

	user := func(fields string) string {
		return `{"data":{"user":{"result":{"__typename":"User","rest_id":"` + typedUserID + `"` + fields + `}}}}`
	}
	allPerspectives := `,"relationship_perspectives":{"following":true,"followed_by":false,"muting":false,"blocking":true,"blocked_by":true}`
	cases := []struct {
		name string
		body string
		want Relationship
		err  error
	}{
		{"legacy_request_flag", user(allPerspectives + `,"legacy":{"follow_request_sent":false}`), Relationship{Following: true, Blocking: true, BlockedBy: true}, nil},
		{"missing_request_flag", user(allPerspectives), Relationship{}, ErrRequestFailed},
		{"missing_perspectives", user(`,"follow_request_sent":false`), Relationship{}, ErrRequestFailed},
		{"partial_perspectives", user(`,"follow_request_sent":false,"relationship_perspectives":{"following":true}`), Relationship{}, ErrRequestFailed},
		{"other_user", `{"data":{"user":{"result":{"__typename":"User","rest_id":"1000000000000000909","follow_request_sent":false` + allPerspectives + `}}}}`, Relationship{}, ErrRequestFailed},
		{"unavailable", `{"data":{"user":{"result":{"__typename":"UserUnavailable","reason":"Suspended"}}}}`, Relationship{}, ErrSuspended},
		{"empty_user", `{"data":{"user":{}}}`, Relationship{}, ErrNotFound},
		{"no_root", `{"data":{}}`, Relationship{}, ErrRequestFailed},
	}
	for _, tc := range cases {
		c, seen := recordingClient(t, respondWith(http.StatusOK, "application/json", tc.body))
		rel, err := c.GetUserRelationship(ctx, typedUserID)
		if len(seen()) != 1 {
			t.Fatalf("%s: %d requests", tc.name, len(seen()))
		}
		if tc.err != nil {
			if !errors.Is(err, tc.err) {
				t.Fatalf("%s: error %v, want %v", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil || rel != tc.want {
			t.Fatalf("%s: %+v %v, want %+v", tc.name, rel, err, tc.want)
		}
	}
}
