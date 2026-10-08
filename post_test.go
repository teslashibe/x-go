package x

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

type writeCall struct {
	name string
	op   string
	call func(context.Context, *Client) (*Tweet, error)
}

var writeCalls = []writeCall{
	{"CreatePost", "CreateTweet", func(ctx context.Context, c *Client) (*Tweet, error) {
		return c.CreatePost(ctx, "hello", WithMediaIDs("m1", "m2"))
	}},
	{"QuotePost", "CreateTweet", func(ctx context.Context, c *Client) (*Tweet, error) {
		return c.QuotePost(ctx, "https://x.com/u1/status/12345", "hello", WithMediaIDs("m1", "m2"))
	}},
	{"ReplyToPost", "CreateTweet", func(ctx context.Context, c *Client) (*Tweet, error) {
		return c.ReplyToPost(ctx, "12345", "hello", WithMediaIDs("m1", "m2"), WithPossiblySensitive())
	}},
}

// writeServer answers every request with status and body and counts requests.
func writeServer(t *testing.T, status int, body string) (*Client, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	return c, &calls
}

func TestWriteErrorCodesAreDefinite(t *testing.T) {
	codes := []struct {
		code    int
		message string
		want    error
	}{
		{185, "User is over daily status update limit.", ErrDailyPostLimit},
		{186, "Tweet needs to be a bit shorter.", ErrTweetTooLong},
		{187, "Status is a duplicate.", ErrDuplicatePost},
		{226, "This request looks like it might be automated. To protect our users from spam and other malicious activity, we can't complete this action right now.", ErrAutomatedRequest},
		{214, "BadRequest: invalid request.", ErrInvalidParams},
		{323, "Only one animated GIF may be attached to a single Tweet.", ErrMediaRejected},
		{324, "The validation of media ids failed.", ErrMediaRejected},
		{325, "A media id was not found.", ErrMediaRejected},
		{326, "To protect our users from spam and other malicious activity, this account is temporarily locked.", ErrChallenge},
		// No recorded 344 body exists; the message here is a placeholder.
		{344, "m", ErrPostingLimited},
		{344, "m", ErrDailyPostLimit},
		{385, "You attempted to reply to a Tweet that is deleted or not visible to you.", ErrReplyRestricted},
		{386, "The Tweet exceeds the number of allowed attachment types.", ErrMediaRejected},
		{433, "The original Tweet author restricted who can reply to this Tweet.", ErrReplyRestricted},
	}
	transports := []struct {
		name   string
		status int
	}{
		{"graphql_200", http.StatusOK},
		{"http_403", http.StatusForbidden},
	}
	ctx := context.Background()
	for _, wc := range writeCalls {
		for _, tr := range transports {
			for _, cc := range codes {
				t.Run(wc.name+"/"+tr.name+"/"+strconv.Itoa(cc.code)+"/"+cc.want.Error(), func(t *testing.T) {
					body, _ := json.Marshal(map[string]any{"errors": []map[string]any{{"code": cc.code, "message": cc.message}}})
					c, calls := writeServer(t, tr.status, string(body))
					tweet, err := wc.call(ctx, c)
					if tweet != nil {
						t.Fatalf("tweet = %#v, want nil", tweet)
					}
					var outcome *OutcomeError
					if !errors.As(err, &outcome) {
						t.Fatalf("error %v is not *OutcomeError", err)
					}
					if !errors.Is(err, ErrDefinite) || errors.Is(err, ErrAmbiguous) {
						t.Fatalf("error %v: want ErrDefinite only", err)
					}
					if !errors.Is(err, cc.want) {
						t.Fatalf("error %v: want %v", err, cc.want)
					}
					if calls.Load() != 1 {
						t.Fatalf("requests = %d, want 1 (writes are never retried)", calls.Load())
					}
				})
			}
		}
	}
}

func TestWriteCodeCasesRunBeforeMessageCases(t *testing.T) {
	ctx := context.Background()
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		body := `{"errors":[{"code":226,"message":"Automated requests are not allowed and the target was not found"}]}`
		c, _ := writeServer(t, status, body)
		_, err := c.CreatePost(ctx, "hello")
		if !errors.Is(err, ErrAutomatedRequest) || errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
			t.Fatalf("status %d: error %v, want ErrAutomatedRequest only", status, err)
		}
	}
}

func TestWriteTransportResetAfterSendIsAmbiguous(t *testing.T) {
	ctx := context.Background()
	for _, wc := range writeCalls {
		t.Run(wc.name, func(t *testing.T) {
			var received atomic.Int32
			c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
				// The request reached the server; drop the connection before
				// any response so the client cannot know the outcome.
				_, _ = io.Copy(io.Discard, r.Body)
				received.Add(1)
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			})
			tweet, err := wc.call(ctx, c)
			if tweet != nil || !errors.Is(err, ErrAmbiguous) || errors.Is(err, ErrDefinite) {
				t.Fatalf("tweet %v error %v, want ErrAmbiguous", tweet, err)
			}
			if received.Load() != 1 {
				t.Fatalf("server received %d requests, want 1", received.Load())
			}
		})
	}
}

func TestWriteMissingPostIDIsAmbiguous(t *testing.T) {
	ctx := context.Background()
	bodies := []string{
		`{"data":{"create_tweet":{"tweet_results":{}}}}`,
		`{"data":{"create_tweet":{"tweet_results":{"result":{"__typename":"Tweet","rest_id":"","legacy":{"full_text":"hello"}}}}}}`,
		`{"data":{}}`,
	}
	for _, wc := range writeCalls {
		for _, body := range bodies {
			c, _ := writeServer(t, http.StatusOK, body)
			tweet, err := wc.call(ctx, c)
			if tweet != nil || !errors.Is(err, ErrAmbiguous) || errors.Is(err, ErrDefinite) {
				t.Errorf("%s %s: tweet %v error %v, want ErrAmbiguous", wc.name, body, tweet, err)
			}
		}
	}

	for _, tweet := range []*Tweet{nil, {}} {
		_, err := writeOutcome(tweet, nil)
		if !errors.Is(err, ErrAmbiguous) || !errors.Is(err, errMissingAck) {
			t.Errorf("writeOutcome(%v) error = %v, want ambiguous missing ack", tweet, err)
		}
	}
}

func TestWriteSuccessRequestShape(t *testing.T) {
	ctx := context.Background()
	const ok = `{"data":{"create_tweet":{"tweet_results":{"result":{"__typename":"Tweet","rest_id":"777","legacy":{"full_text":"hello","conversation_id_str":"777"}}}}}}`
	for _, wc := range writeCalls {
		t.Run(wc.name, func(t *testing.T) {
			var payload struct {
				Variables map[string]json.RawMessage `json:"variables"`
			}
			c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/"+wc.op) {
					t.Errorf("request %s %s, want POST .../%s", r.Method, r.URL.Path, wc.op)
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				_, _ = io.WriteString(w, ok)
			})
			tweet, err := wc.call(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			if tweet.ID != "777" {
				t.Fatalf("tweet ID = %q, want 777", tweet.ID)
			}
			var media struct {
				Entities []struct {
					MediaID string `json:"media_id"`
				} `json:"media_entities"`
				PossiblySensitive bool `json:"possibly_sensitive"`
			}
			if err := json.Unmarshal(payload.Variables["media"], &media); err != nil {
				t.Fatal(err)
			}
			if len(media.Entities) != 2 || media.Entities[0].MediaID != "m1" || media.Entities[1].MediaID != "m2" {
				t.Fatalf("media = %#v", media)
			}
			switch wc.name {
			case "QuotePost":
				if string(payload.Variables["attachment_url"]) != `"https://x.com/u1/status/12345"` {
					t.Fatalf("attachment_url = %s", payload.Variables["attachment_url"])
				}
			case "ReplyToPost":
				if !strings.Contains(string(payload.Variables["reply"]), `"in_reply_to_tweet_id":"12345"`) || !media.PossiblySensitive {
					t.Fatalf("reply = %s, sensitive %v", payload.Variables["reply"], media.PossiblySensitive)
				}
			case "CreatePost":
				if _, ok := payload.Variables["reply"]; ok {
					t.Fatal("original post carries a reply target")
				}
				if _, ok := payload.Variables["attachment_url"]; ok {
					t.Fatal("original post carries an attachment_url")
				}
			}
		})
	}
}

func TestWriteValidationFailuresAreDefinite(t *testing.T) {
	ctx := context.Background()
	c, calls := writeServer(t, http.StatusOK, `{}`)
	long := strings.Repeat("a", maxTweetLengthFree+1)
	cases := []func() (*Tweet, error){
		func() (*Tweet, error) { return c.CreatePost(ctx, "") },
		func() (*Tweet, error) { return c.QuotePost(ctx, "", "hello") },
		func() (*Tweet, error) { return c.CreatePost(ctx, long) },
	}
	for i, call := range cases {
		if _, err := call(); !errors.Is(err, ErrDefinite) {
			t.Errorf("case %d: error %v, want ErrDefinite", i, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("validation failures sent %d requests", calls.Load())
	}
}

func TestClassifyXErrorCodeSharedByRESTBodies(t *testing.T) {
	for code, want := range map[int]error{
		32: ErrUnauthorized, 34: ErrNotFound, 63: ErrSuspended, 88: ErrRateLimited, 144: ErrNotFound,
		185: ErrDailyPostLimit, 186: ErrTweetTooLong, 187: ErrDuplicatePost, 214: ErrInvalidParams,
		226: ErrAutomatedRequest, 323: ErrMediaRejected, 324: ErrMediaRejected, 325: ErrMediaRejected,
		326: ErrChallenge, 327: ErrAlreadyRetweeted, 344: ErrPostingLimited, 349: ErrDMClosed,
		385: ErrReplyRestricted, 386: ErrMediaRejected, 433: ErrReplyRestricted,
	} {
		body := `{"errors":[{"code":` + strconv.Itoa(code) + `,"message":"m"}]}`
		if got := classifyRESTErrorBody([]byte(body)); !errors.Is(got, want) {
			t.Errorf("REST code %d = %v, want %v", code, got, want)
		}
		if got := classifyGQLError(gqlError{Code: code, Message: "m"}); !errors.Is(got, want) {
			t.Errorf("GraphQL code %d = %v, want %v", code, got, want)
		}
	}
	if got := classifyRESTErrorBody([]byte(`{"errors":[{"code":131,"message":"Internal error"}]}`)); got != nil {
		t.Errorf("unmapped REST code = %v, want nil", got)
	}
}

func TestPostingLimitedMatchesDailyLimit(t *testing.T) {
	err := classifyXErrorCode(344)
	if !errors.Is(err, ErrPostingLimited) || !errors.Is(err, ErrDailyPostLimit) || err.Error() != ErrPostingLimited.Error() {
		t.Fatalf("344 = %v, want ErrPostingLimited that also matches ErrDailyPostLimit", err)
	}
	if errors.Is(classifyXErrorCode(185), ErrPostingLimited) {
		t.Fatal("185 must not match ErrPostingLimited")
	}
}

func TestGraphQLErrorCodeFromExtensions(t *testing.T) {
	got := classifyGQLError(gqlError{Message: "Authorization: Status is a duplicate.", Extensions: struct {
		Code int    `json:"code"`
		Kind string `json:"kind"`
	}{Code: 187, Kind: "Permissions"}})
	if !errors.Is(got, ErrDuplicatePost) {
		t.Fatalf("extensions code 187 = %v, want ErrDuplicatePost", got)
	}
}

// createdBody is a compose response for post 555.
const createdBody = `"data":{"create_tweet":{"tweet_results":{"result":{"__typename":"Tweet","rest_id":"555","legacy":{"full_text":"hello","conversation_id_str":"555"}}}}}`

func TestWriteCreatedPostWithErrorsSucceeds(t *testing.T) {
	ctx := context.Background()
	errs := []string{
		// A quote whose own quoted post is gone.
		`[{"message":"_Missing: No status found with that ID.","code":144,"kind":"NonFatal","path":["create_tweet","tweet_results","result","quoted_status_result","result"]}]`,
		`[{"message":"_Missing: No status found with that ID.","code":144}]`,
	}
	for _, wc := range writeCalls {
		for _, e := range errs {
			c, calls := writeServer(t, http.StatusOK, `{`+createdBody+`,"errors":`+e+`}`)
			tweet, err := wc.call(ctx, c)
			if err != nil || tweet == nil || tweet.ID != "555" {
				t.Errorf("%s: tweet %v error %v, want post 555", wc.name, tweet, err)
			}
			if calls.Load() != 1 {
				t.Errorf("%s: %d requests, want 1", wc.name, calls.Load())
			}
		}
	}
	// The legacy compose methods agree.
	c, _ := writeServer(t, http.StatusOK, `{`+createdBody+`,"errors":`+errs[0]+`}`)
	if tweet, err := c.CreateTweet(ctx, "hello"); err != nil || tweet.ID != "555" {
		t.Fatalf("CreateTweet: tweet %v error %v", tweet, err)
	}
}

func TestWriteErrorsBesidePostWithoutIDAreAmbiguous(t *testing.T) {
	ctx := context.Background()
	body := `{"data":{"create_tweet":{"tweet_results":{"result":{"__typename":"Tweet","rest_id":"","legacy":{"full_text":"hello"}}}}},` +
		`"errors":[{"message":"_Missing: No status found with that ID.","code":144}]}`
	for _, wc := range writeCalls {
		c, _ := writeServer(t, http.StatusOK, body)
		tweet, err := wc.call(ctx, c)
		if tweet != nil || !errors.Is(err, ErrAmbiguous) || errors.Is(err, ErrDefinite) || errors.Is(err, ErrNotFound) {
			t.Errorf("%s: tweet %v error %v, want ErrAmbiguous only", wc.name, tweet, err)
		}
	}
	// An empty create result next to an error is a definite rejection.
	for _, data := range []string{`{}`, `{"create_tweet":{"tweet_results":{}}}`, `{"create_tweet":{"tweet_results":{"result":null}}}`} {
		c, _ := writeServer(t, http.StatusOK, `{"data":`+data+`,"errors":[{"message":"Authorization: automated (226)","code":226,"kind":"Permissions","path":["create_tweet"]}]}`)
		_, err := c.CreatePost(ctx, "hello")
		if !errors.Is(err, ErrDefinite) || !errors.Is(err, ErrAutomatedRequest) {
			t.Errorf("data %s: error %v, want definite ErrAutomatedRequest", data, err)
		}
	}
}

func TestEnforcementCodesAreNotRetried(t *testing.T) {
	ctx := context.Background()
	for _, code := range []int{185, 186, 187, 214, 226, 323, 324, 325, 327, 344, 349, 385, 386, 433} {
		for _, status := range []int{http.StatusOK, http.StatusForbidden} {
			var calls atomic.Int32
			c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"errors":[{"code":`+strconv.Itoa(code)+`,"message":"m"}]}`)
			})
			c.maxRetries = 3
			c.retryBase = 0
			_, err := c.GetTweet(ctx, "12345")
			if err == nil || calls.Load() != 1 {
				t.Errorf("code %d status %d: %d requests (error %v), want 1", code, status, calls.Load(), err)
			}
			if code == 226 && status == http.StatusForbidden && (errors.Is(err, ErrForbidden) || !errors.Is(err, ErrAutomatedRequest)) {
				t.Errorf("403+226 = %v, want ErrAutomatedRequest only", err)
			}
		}
	}
	// A transient failure is still retried.
	var calls atomic.Int32
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	c.maxRetries = 3
	c.retryBase = 0
	if _, err := c.GetTweet(ctx, "12345"); err == nil || calls.Load() != 3 {
		t.Errorf("502: %d requests (error %v), want 3", calls.Load(), err)
	}
}
