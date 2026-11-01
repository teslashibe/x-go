package x

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Typed writes (LikePost ... UnblockUser, DeletePost) share one transport and
// one set of outcome rules:
//
//   - Exactly one request, never retried, sent through a per-call client that
//     refuses redirects. The shared client used by Like, Follow, DeleteTweet
//     and the other existing methods is not changed.
//   - Nothing is sent, and the error is NotAttempted, when the ID is invalid,
//     the operation has no query id, or ctx ends before sending.
//   - A GraphQL write is applied only when the response has no errors and its
//     data carries the operation's ack root with a value. An ack root is
//     registered only from a recorded provider capture, and none exists yet,
//     so today a GraphQL success response without errors is ambiguous.
//   - A form POST is applied only on a 200 JSON user whose id_str equals the
//     target. For FollowUser, follow_request_sent true gives WriteRequested.
//   - A redirect, a non-200 2xx, a transport failure after sending, a 5xx, an
//     unknown error code or a response that does not confirm the change is
//     ambiguous. Known X rejections (for example 226, 144, 88 or HTTP 429) are
//     definite, as classifyWriteOutcome already decides for CreatePost.
//   - Code 327 keeps its existing mapping to ErrAlreadyRetweeted and is never
//     read as WriteAlreadyInState: no recorded capture shows what it means for
//     these operations.

// gqlWriteOp describes one typed GraphQL write.
type gqlWriteOp struct {
	method    string // SDK method name, reported as Operation
	operation string // GraphQL operation name, looked up in queryIDs
	idVar     string // variable carrying the target ID
	extra     map[string]interface{}
	// ackRoot is the data field a recorded capture showed X returns when the
	// write is applied. Empty means no capture exists and success is
	// unconfirmable.
	ackRoot string
}

var (
	likePostOp       = gqlWriteOp{method: "LikePost", operation: "FavoriteTweet", idVar: "tweet_id"}
	unlikePostOp     = gqlWriteOp{method: "UnlikePost", operation: "UnfavoriteTweet", idVar: "tweet_id"}
	repostOp         = gqlWriteOp{method: "Repost", operation: "CreateRetweet", idVar: "tweet_id", extra: map[string]interface{}{"dark_request": false}}
	unrepostOp       = gqlWriteOp{method: "Unrepost", operation: "DeleteRetweet", idVar: "source_tweet_id", extra: map[string]interface{}{"dark_request": false}}
	bookmarkPostOp   = gqlWriteOp{method: "BookmarkPost", operation: "CreateBookmark", idVar: "tweet_id"}
	unbookmarkPostOp = gqlWriteOp{method: "UnbookmarkPost", operation: "DeleteBookmark", idVar: "tweet_id"}
	deletePostOp     = gqlWriteOp{method: "DeletePost", operation: "DeleteTweet", idVar: "tweet_id", extra: map[string]interface{}{"dark_request": false}}
)

// formWriteOp describes one typed REST form POST on a user.
type formWriteOp struct {
	method string
	path   string
	// followRequest reports that follow_request_sent true means a pending
	// follow request (only for friendships/create).
	followRequest bool
}

var (
	followUserOp   = formWriteOp{method: "FollowUser", path: "/i/api/1.1/friendships/create.json", followRequest: true}
	unfollowUserOp = formWriteOp{method: "UnfollowUser", path: "/i/api/1.1/friendships/destroy.json"}
	muteUserOp     = formWriteOp{method: "MuteUser", path: "/i/api/1.1/mutes/users/create.json"}
	unmuteUserOp   = formWriteOp{method: "UnmuteUser", path: "/i/api/1.1/mutes/users/destroy.json"}
	blockUserOp    = formWriteOp{method: "BlockUser", path: "/i/api/1.1/blocks/create.json"}
	unblockUserOp  = formWriteOp{method: "UnblockUser", path: "/i/api/1.1/blocks/destroy.json"}
)

// xNumericID reports whether v is a canonical X numeric ID
// (^[1-9][0-9]{0,19}$).
func xNumericID(v string) bool {
	if v == "" || len(v) > 20 || v[0] == '0' {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func notAttempted(method string, err error) *OutcomeError {
	return &OutcomeError{Operation: method, Outcome: ErrDefinite, NotAttempted: true, Err: errors.Join(errWriteNotAttempted, err)}
}

func ambiguousWrite(method string, status, code int, err error) *OutcomeError {
	return &OutcomeError{Operation: method, Outcome: ErrAmbiguous, Ambiguous: true, Status: status, Code: code, Err: err}
}

// classifiedWrite applies classifyWriteOutcome's definite/ambiguous rule to
// err and records the evidence.
func classifiedWrite(method string, status, code int, err error) *OutcomeError {
	outcome := ErrAmbiguous
	var classified *OutcomeError
	if errors.As(classifyWriteOutcome(err), &classified) && classified.Outcome == ErrDefinite {
		outcome = ErrDefinite
	}
	return &OutcomeError{Operation: method, Outcome: outcome, Ambiguous: outcome == ErrAmbiguous, Status: status, Code: code, Err: err}
}

// noRedirectClient keeps the shared transport, jar and timeout but refuses
// redirects locally, so one call is one physical request and cookies are never
// forwarded to a redirect target (pattern: chatGET).
func (c *Client) noRedirectClient() *http.Client {
	return &http.Client{
		Transport:     c.httpClient.Transport,
		Jar:           c.httpClient.Jar,
		Timeout:       c.httpClient.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// singleResponse is one provider response with its body read.
type singleResponse struct {
	req    *http.Request
	resp   *http.Response
	body   []byte
	status int
}

// sendOnce waits for the request gap, then sends exactly one request without
// following redirects. A nil error with notSent false means a response
// arrived; notSent reports that ctx ended before the request was sent.
func (c *Client) sendOnce(ctx context.Context, method, endpoint, contentType string, body []byte) (r singleResponse, notSent bool, err error) {
	c.waitForGap(ctx)
	if err := ctx.Err(); err != nil {
		return r, true, err
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return r, true, fmt.Errorf("%w: building request: %v", ErrInvalidParams, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.setHeaders(req)
	resp, err := c.noRedirectClient().Do(req)
	if err != nil {
		return r, false, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return r, false, fmt.Errorf("%w: reading body: %v", ErrRequestFailed, err)
	}
	if len(data) > maxResponseBody {
		return r, false, fmt.Errorf("%w: response body too large", ErrRequestFailed)
	}
	// checkStatus reads the body again on non-200 answers.
	resp.Body = io.NopCloser(bytes.NewReader(data))
	return singleResponse{req: req, resp: resp, body: data, status: resp.StatusCode}, false, nil
}

// xErrorEnvelope returns the errors X put in a JSON body, if any.
func xErrorEnvelope(body []byte) []gqlError {
	var envelope struct {
		Errors []gqlError `json:"errors"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return nil
	}
	return envelope.Errors
}

func firstXErrorCode(body []byte) int {
	if errs := xErrorEnvelope(body); len(errs) > 0 {
		return errs[0].code()
	}
	return 0
}

// rateLimited records a provider cooldown for err when it is a rate limit
// and returns it as a *RateLimitError.
func (c *Client) rateLimited(err error, h http.Header) error {
	if !errors.Is(err, ErrRateLimited) {
		return err
	}
	var rle *RateLimitError
	if errors.As(err, &rle) {
		return err
	}
	wait := parseRetryAfter(h.Get("Retry-After"), 0)
	if wait == 0 {
		wait = parseRetryAfter(rlHeader(h, "Reset"), 0)
	}
	if wait <= 0 {
		wait = 60 * time.Second
	}
	c.recordRateLimit(wait)
	return &RateLimitError{Wait: wait}
}

// statusError classifies a non-200 response. ok is false for a 200.
// A 404 without an X error body on a GraphQL path is a stale query id, never
// ErrNotFound: only X error codes (34, 144, ...) prove an absent target.
func (c *Client) statusError(r singleResponse, graphql bool) (err error, redirect, bare404 bool) {
	if r.status >= 300 && r.status < 400 {
		c.updateRateLimit(r.resp)
		return fmt.Errorf("%w: %w: HTTP %d", ErrRequestFailed, errRedirectRefused, r.status), true, false
	}
	if r.status == http.StatusNotFound && len(xErrorEnvelope(r.body)) == 0 {
		c.updateRateLimit(r.resp)
		if graphql {
			return fmt.Errorf("%w: HTTP 404 without an X error body", ErrQueryIDStale), false, true
		}
		return fmt.Errorf("%w: HTTP 404 without an X error body", ErrRequestFailed), false, true
	}
	if err := c.checkStatus(r.resp); err != nil {
		return c.rateLimited(err, r.resp.Header), false, false
	}
	return nil, false, false
}

// graphqlWrite sends one typed GraphQL write and applies the outcome rules.
func (c *Client) graphqlWrite(ctx context.Context, op gqlWriteOp, id string) (WriteResult, error) {
	if !xNumericID(id) {
		return WriteResult{}, notAttempted(op.method, ErrInvalidParams)
	}
	qid := c.queryID(op.operation)
	if qid == "" {
		return WriteResult{}, notAttempted(op.method, ErrQueryIDUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return WriteResult{}, notAttempted(op.method, err)
	}
	vars := map[string]interface{}{op.idVar: id}
	for k, v := range op.extra {
		vars[k] = v
	}
	payload, err := json.Marshal(map[string]interface{}{
		"variables": vars,
		"features":  c.features,
		"queryId":   qid,
	})
	if err != nil {
		return WriteResult{}, notAttempted(op.method, fmt.Errorf("%w: marshalling body: %v", ErrInvalidParams, err))
	}
	endpoint := fmt.Sprintf("%s/%s/%s", graphqlBase, qid, op.operation)
	r, notSent, err := c.sendOnce(ctx, http.MethodPost, endpoint, "application/json", payload)
	if notSent {
		return WriteResult{}, notAttempted(op.method, err)
	}
	if err != nil {
		return WriteResult{}, ambiguousWrite(op.method, 0, 0, err)
	}
	if err, redirect, bare404 := c.statusError(r, true); err != nil {
		if redirect {
			return WriteResult{}, ambiguousWrite(op.method, r.status, 0, err)
		}
		if bare404 {
			// A stale query id: X routed no operation, so nothing ran.
			return WriteResult{}, &OutcomeError{Operation: op.method, Outcome: ErrDefinite, Status: r.status, Err: err}
		}
		return WriteResult{}, classifiedWrite(op.method, r.status, firstXErrorCode(r.body), err)
	}
	envelope, err := decodeGQLResponse(r.body)
	if err != nil {
		return WriteResult{}, ambiguousWrite(op.method, r.status, 0, err)
	}
	if len(envelope.Errors) > 0 {
		code := envelope.Errors[0].code()
		gqlErr := c.rateLimited(classifyGQLError(envelope.Errors[0]), r.resp.Header)
		if usableData(envelope.Data, "") {
			// Data next to errors: the write may have run.
			return WriteResult{}, ambiguousWrite(op.method, r.status, code, gqlErr)
		}
		return WriteResult{}, classifiedWrite(op.method, r.status, code, gqlErr)
	}
	if op.ackRoot == "" || !usableData(envelope.Data, op.ackRoot) {
		return WriteResult{}, ambiguousWrite(op.method, r.status, 0, errUnconfirmedWrite)
	}
	return WriteResult{Operation: op.method, TargetID: id, State: WriteApplied}, nil
}

// formWrite sends one typed REST form POST and applies the outcome rules.
func (c *Client) formWrite(ctx context.Context, op formWriteOp, userID string) (WriteResult, error) {
	if !xNumericID(userID) {
		return WriteResult{}, notAttempted(op.method, ErrInvalidParams)
	}
	if err := ctx.Err(); err != nil {
		return WriteResult{}, notAttempted(op.method, err)
	}
	form := url.Values{}
	form.Set("user_id", userID)
	r, notSent, err := c.sendOnce(ctx, http.MethodPost, baseURL+op.path, "application/x-www-form-urlencoded", []byte(form.Encode()))
	if notSent {
		return WriteResult{}, notAttempted(op.method, err)
	}
	if err != nil {
		return WriteResult{}, ambiguousWrite(op.method, 0, 0, err)
	}
	if err, redirect, bare404 := c.statusError(r, false); err != nil {
		if redirect || bare404 {
			return WriteResult{}, ambiguousWrite(op.method, r.status, 0, err)
		}
		return WriteResult{}, classifiedWrite(op.method, r.status, firstXErrorCode(r.body), err)
	}
	if errs := xErrorEnvelope(r.body); len(errs) > 0 {
		code := errs[0].code()
		restErr := classifyRESTErrorBody(r.body)
		if restErr == nil {
			restErr = fmt.Errorf("%w: X error code %d", ErrRequestFailed, code)
		}
		return WriteResult{}, classifiedWrite(op.method, r.status, code, c.rateLimited(restErr, r.resp.Header))
	}
	if mediaType, _, err := mime.ParseMediaType(r.resp.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
		return WriteResult{}, ambiguousWrite(op.method, r.status, 0, fmt.Errorf("%w: response is not JSON", errUnconfirmedWrite))
	}
	var user struct {
		IDStr             string `json:"id_str"`
		FollowRequestSent *bool  `json:"follow_request_sent"`
	}
	if err := json.Unmarshal(r.body, &user); err != nil || user.IDStr != userID {
		return WriteResult{}, ambiguousWrite(op.method, r.status, 0, fmt.Errorf("%w: response user does not match the target", errUnconfirmedWrite))
	}
	state := WriteApplied
	if op.followRequest && user.FollowRequestSent != nil && *user.FollowRequestSent {
		state = WriteRequested
	}
	return WriteResult{Operation: op.method, TargetID: userID, State: state}, nil
}

// graphqlReadOnce sends one GraphQL GET for a state read: no retries, no
// redirects, and a bare 404 is ErrQueryIDStale rather than ErrNotFound.
func (c *Client) graphqlReadOnce(ctx context.Context, operation string, variables map[string]interface{}) (json.RawMessage, error) {
	qid := c.queryID(operation)
	if qid == "" {
		return nil, fmt.Errorf("%w: %s", ErrQueryIDUnavailable, operation)
	}
	varsJSON, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("%w: marshalling variables: %v", ErrInvalidParams, err)
	}
	featsJSON, err := json.Marshal(c.features)
	if err != nil {
		return nil, fmt.Errorf("%w: marshalling features: %v", ErrInvalidParams, err)
	}
	params := url.Values{}
	params.Set("variables", string(varsJSON))
	params.Set("features", string(featsJSON))
	endpoint := fmt.Sprintf("%s/%s/%s?%s", graphqlBase, qid, operation, params.Encode())
	r, _, err := c.sendOnce(ctx, http.MethodGet, endpoint, "", nil)
	if err != nil {
		return nil, err
	}
	if err, _, _ := c.statusError(r, true); err != nil {
		return nil, c.operationError(operation, r.req, r.resp, err)
	}
	envelope, err := decodeGQLResponse(r.body)
	if err != nil {
		return nil, err
	}
	data, err := readGQLData(r.body, envelope)
	if err != nil {
		return nil, c.rateLimited(err, r.resp.Header)
	}
	return data, nil
}

// errStateUnverifiable marks a state read whose response lacks the fields
// needed to report the state; the caller must not guess.
func errStateUnverifiable(what string) error {
	return fmt.Errorf("%w: %s", ErrRequestFailed, strings.TrimSpace(what))
}
