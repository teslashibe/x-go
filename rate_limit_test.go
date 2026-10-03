package x

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func quotaResponse(remaining int, reset time.Time) *http.Response {
	r := jsonResponse(http.StatusOK, `{"data":{}}`)
	r.Header.Set("X-Rate-Limit-Limit", "100")
	r.Header.Set("X-Rate-Limit-Remaining", strconv.Itoa(remaining))
	r.Header.Set("X-Rate-Limit-Reset", strconv.FormatInt(reset.Unix(), 10))
	return r
}

func TestGraphQLQuotaDoesNotThrottleAnotherOperation(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Path == "/i/api/graphql/viewer/Viewer" {
			return quotaResponse(0, time.Now().Add(time.Hour)), nil
		}
		return jsonResponse(http.StatusOK, `{"data":{}}`), nil
	}))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.doGraphQLGET(ctx, "viewer", "Viewer", []byte(`{}`), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.doGraphQLGET(ctx, "profile", "UserByRestId", []byte(`{}`), []byte(`{}`)); err != nil {
		t.Fatal("Viewer quota leaked into profile operation", err)
	}
	blocked, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if _, err := c.doGraphQLGET(blocked, "viewer", "Viewer", []byte(`{}`), []byte(`{}`)); !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 2 {
		t.Fatal("exhausted endpoint executed before reset", err, calls.Load())
	}
	c.gapMu.Lock()
	reserved := c.operationSlots["Viewer"]
	c.gapMu.Unlock()
	if reserved.After(time.Now().Add(time.Second)) {
		t.Fatal("cancelled endpoint reservation was retained")
	}
	if _, err := c.doGraphQLGET(ctx, "profile", "UserByRestId", []byte(`{}`), []byte(`{}`)); err != nil || calls.Load() != 3 {
		t.Fatal("cancelled quota wait blocked independent operation", err, calls.Load())
	}
}

func TestGraphQLQuotaPreservesPerOperationObservations(t *testing.T) {
	c := &Client{minGap: time.Millisecond}
	r := quotaResponse(1, time.Now().Add(time.Hour))
	c.updateRateLimit(r, "Viewer")
	r.Body.Close()
	if got := c.RateLimit(); got.Limit != 100 || got.Remaining != 1 || got.Reset.IsZero() {
		t.Fatal("public quota snapshot changed", got)
	}
	r = jsonResponse(http.StatusOK, `{"data":{}}`)
	c.updateRateLimit(r, "UserByRestId")
	r.Body.Close()
	c.rlMu.Lock()
	viewer, profile := c.operationRates["Viewer"], c.operationRates["UserByRestId"]
	c.rlMu.Unlock()
	if viewer.Remaining != 1 || viewer.Reset.IsZero() || profile != (RateLimitState{}) {
		t.Fatal("operation observations were mixed")
	}
}

func TestGraphQL429RetainsAccountCooldown(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		resp := quotaResponse(0, time.Now().Add(time.Hour))
		resp.StatusCode = http.StatusTooManyRequests
		return resp, nil
	}))
	_, err := c.doGraphQLGET(context.Background(), "viewer", "Viewer", []byte(`{}`), []byte(`{}`))
	var limited *RateLimitError
	if !errors.As(err, &limited) || limited.Wait <= 0 {
		t.Fatal("429 cooldown was lost", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = c.doGraphQLGET(ctx, "profile", "UserByRestId", []byte(`{}`), []byte(`{}`))
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatal("another endpoint bypassed the account cooldown", err, calls.Load())
	}
}

func TestConcurrentGraphQLReadsKeepSharedMinimumGap(t *testing.T) {
	var mu sync.Mutex
	var arrivals []time.Time
	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		arrivals = append(arrivals, time.Now())
		mu.Unlock()
		return jsonResponse(http.StatusOK, `{"data":{}}`), nil
	}))
	c.minGap = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			operation := "Viewer"
			if i%2 == 0 {
				operation = "UserByRestId"
			}
			if _, err := c.doGraphQLGET(ctx, "q", operation, []byte(`{}`), []byte(`{}`)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if len(arrivals) != 4 || arrivals[3].Sub(arrivals[0]) < 25*time.Millisecond {
		t.Fatal("concurrent operations lost shared request pacing")
	}
}

func TestQueuedGraphQLReadObservesNew429Cooldown(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	c := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
			time.Sleep(50 * time.Millisecond)
			resp := quotaResponse(0, time.Now().Add(time.Hour))
			resp.StatusCode = http.StatusTooManyRequests
			return resp, nil
		}
		return jsonResponse(http.StatusOK, `{"data":{}}`), nil
	}))
	c.minGap = 100 * time.Millisecond
	firstDone := make(chan error, 1)
	go func() {
		_, err := c.doGraphQLGET(context.Background(), "viewer", "Viewer", []byte(`{}`), []byte(`{}`))
		firstDone <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_, err := c.doGraphQLGET(ctx, "profile", "UserByRestId", []byte(`{}`), []byte(`{}`))
	var limited *RateLimitError
	if !errors.As(<-firstDone, &limited) || !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatal("queued operation bypassed the new account cooldown", err, calls.Load())
	}
}
