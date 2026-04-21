package x

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBody = 10 << 20 // 10 MB

// graphqlGET executes an authenticated GraphQL GET request with retries.
func (c *Client) graphqlGET(ctx context.Context, operationName string, variables map[string]interface{}) (json.RawMessage, error) {
	qid := c.queryID(operationName)
	if qid == "" {
		return nil, fmt.Errorf("%w: no queryId registered for %q", ErrInvalidParams, operationName)
	}

	varsJSON, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("%w: marshalling variables: %v", ErrInvalidParams, err)
	}

	featsJSON, err := json.Marshal(c.features)
	if err != nil {
		return nil, fmt.Errorf("%w: marshalling features: %v", ErrInvalidParams, err)
	}

	attempts := c.maxRetries
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			wait := c.retryBase * time.Duration(math.Pow(2, float64(i-1)))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		data, err := c.doGraphQLGET(ctx, qid, operationName, varsJSON, featsJSON)
		if err == nil {
			return data, nil
		}

		if isNonRetriable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// graphqlPOST executes an authenticated GraphQL POST request with retries.
func (c *Client) graphqlPOST(ctx context.Context, operationName string, variables map[string]interface{}) (json.RawMessage, error) {
	qid := c.queryID(operationName)
	if qid == "" {
		return nil, fmt.Errorf("%w: no queryId registered for %q", ErrInvalidParams, operationName)
	}

	attempts := c.maxRetries
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			wait := c.retryBase * time.Duration(math.Pow(2, float64(i-1)))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		data, err := c.doGraphQLPOST(ctx, qid, operationName, variables)
		if err == nil {
			return data, nil
		}

		if isNonRetriable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// doGraphQLGET performs a single GraphQL GET request.
func (c *Client) doGraphQLGET(ctx context.Context, qid, operationName string, varsJSON, featsJSON []byte) (json.RawMessage, error) {
	c.waitForGap(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	endpoint := fmt.Sprintf("%s/%s/%s", graphqlBase, qid, operationName)
	params := url.Values{}
	params.Set("variables", string(varsJSON))
	params.Set("features", string(featsJSON))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", ErrRequestFailed, err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()

	if err := c.checkStatus(resp); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("%w: reading body: %v", ErrRequestFailed, err)
	}

	return c.parseGQLResponse(body)
}

// doGraphQLPOST performs a single GraphQL POST request.
func (c *Client) doGraphQLPOST(ctx context.Context, qid, operationName string, variables map[string]interface{}) (json.RawMessage, error) {
	c.waitForGap(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	endpoint := fmt.Sprintf("%s/%s/%s", graphqlBase, qid, operationName)

	payload := map[string]interface{}{
		"variables": variables,
		"features":  c.features,
		"queryId":   qid,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: marshalling body: %v", ErrRequestFailed, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", ErrRequestFailed, err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()

	if err := c.checkStatus(resp); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("%w: reading body: %v", ErrRequestFailed, err)
	}

	return c.parseGQLResponse(body)
}

// restGET performs an authenticated REST API GET request.
func (c *Client) restGET(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	c.waitForGap(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	endpoint := baseURL + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", ErrRequestFailed, err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()

	if err := c.checkStatus(resp); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("%w: reading body: %v", ErrRequestFailed, err)
	}

	return body, nil
}

// restPOST performs an authenticated REST API POST request.
func (c *Client) restPOST(ctx context.Context, path string, payload interface{}) (json.RawMessage, error) {
	c.waitForGap(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: marshalling body: %v", ErrRequestFailed, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", ErrRequestFailed, err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()

	if err := c.checkStatus(resp); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("%w: reading body: %v", ErrRequestFailed, err)
	}

	return body, nil
}

// restFormPOST performs an authenticated form-encoded POST to a REST endpoint.
func (c *Client) restFormPOST(ctx context.Context, path string, form url.Values) (json.RawMessage, error) {
	c.waitForGap(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", ErrRequestFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()

	if err := c.checkStatus(resp); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("%w: reading body: %v", ErrRequestFailed, err)
	}

	return body, nil
}

// setHeaders sets all required auth and fingerprint headers on the request.
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("X-Csrf-Token", c.cookies.CT0)
	req.Header.Set("X-Twitter-Active-User", "yes")
	req.Header.Set("X-Twitter-Auth-Type", "OAuth2Session")
	req.Header.Set("X-Twitter-Client-Language", "en")
	req.Header.Set("Referer", baseURL+"/")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Cookie", c.cookieHeader())
	if txID := c.generateTransactionID(req.Method, req.URL.Path); txID != "" {
		req.Header.Set("X-Client-Transaction-Id", txID)
	}
}

// cookieHeader builds the Cookie header value.
func (c *Client) cookieHeader() string {
	var b strings.Builder
	add := func(name, val string) {
		if val == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(val)
	}
	add("auth_token", c.cookies.AuthToken)
	add("ct0", c.cookies.CT0)
	add("twid", c.cookies.Twid)
	add("kdt", c.cookies.KDT)
	return b.String()
}

// queryID performs a thread-safe lookup of the queryId for a given operation.
func (c *Client) queryID(name string) string {
	c.reqMu.RLock()
	defer c.reqMu.RUnlock()
	return c.queryIDs[name]
}

// waitForGap enforces the leaky-bucket minimum request gap.
func (c *Client) waitForGap(ctx context.Context) {
	c.gapMu.Lock()
	now := time.Now()
	nextSlot := c.lastReqAt.Add(c.minGap)
	if now.After(nextSlot) {
		nextSlot = now
	}
	c.lastReqAt = nextSlot
	c.gapMu.Unlock()

	if wait := time.Until(nextSlot); wait > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

// checkStatus maps HTTP status codes to sentinel errors.
// On non-OK responses, it drains the body so the TCP connection can be reused.
func (c *Client) checkStatus(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// Drain body to allow keep-alive reuse.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBody))

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		return ErrForbidden
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		// X uses x-rate-limit-reset (Unix timestamp) and sometimes Retry-After.
		wait := parseRetryAfter(resp.Header.Get("X-Rate-Limit-Reset"), 0)
		if wait == 0 {
			wait = parseRetryAfter(resp.Header.Get("Retry-After"), 60*time.Second)
		}
		return fmt.Errorf("%w (retry after %s)", ErrRateLimited, wait)
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: HTTP %d", ErrRequestFailed, resp.StatusCode)
	default:
		return fmt.Errorf("%w: unexpected HTTP %d", ErrRequestFailed, resp.StatusCode)
	}
}

// parseGQLResponse extracts the data field from a GraphQL response.
func (c *Client) parseGQLResponse(body []byte) (json.RawMessage, error) {
	var envelope gqlResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: decoding response: %v (snippet: %s)", ErrRequestFailed, err, truncate(string(body), 300))
	}

	if len(envelope.Errors) > 0 {
		first := envelope.Errors[0]
		msg := strings.ToLower(first.Message)
		switch {
		case first.Code == 32 || strings.Contains(msg, "not authenticated"):
			return nil, ErrUnauthorized
		case first.Code == 63 || strings.Contains(msg, "suspended"):
			return nil, ErrSuspended
		case first.Code == 34 || strings.Contains(msg, "not found"):
			return nil, ErrNotFound
		case first.Code == 88 || strings.Contains(msg, "rate limit"):
			return nil, ErrRateLimited
		case first.Code == 327 || strings.Contains(msg, "already retweeted"):
			return nil, ErrAlreadyRetweeted
		case first.Code == 349 || strings.Contains(msg, "send a direct message"):
			return nil, ErrDMClosed
		case strings.Contains(msg, "forbidden") || strings.Contains(msg, "not allowed"):
			return nil, ErrForbidden
		default:
			return nil, fmt.Errorf("%w: %s (code %d)", ErrRequestFailed, first.Message, first.Code)
		}
	}

	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, fmt.Errorf("%w: no data in response (snippet: %s)", ErrRequestFailed, truncate(string(body), 300))
	}

	return envelope.Data, nil
}

// parseRetryAfter parses rate-limit headers. Handles three formats:
// - Seconds integer (Retry-After: 60)
// - Unix epoch timestamp (X-Rate-Limit-Reset: 1716000000)
// - HTTP-date (Retry-After: Mon, 01 Jan 2024 00:00:00 GMT)
func parseRetryAfter(val string, fallback time.Duration) time.Duration {
	if val == "" {
		return fallback
	}
	trimmed := strings.TrimSpace(val)
	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		if n > 1_000_000_000 {
			// Unix timestamp — compute duration until that time.
			if d := time.Until(time.Unix(n, 0)); d > 0 {
				return d
			}
			return fallback
		}
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(trimmed); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return fallback
}

// isNonRetriable reports whether err should not be retried.
func isNonRetriable(err error) bool {
	return errors.Is(err, ErrInvalidAuth) ||
		errors.Is(err, ErrUnauthorized) ||
		errors.Is(err, ErrForbidden) ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrSuspended) ||
		errors.Is(err, ErrInvalidParams) ||
		errors.Is(err, ErrQueryIDStale)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
