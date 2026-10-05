package x

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func browserFixtureOperation() BrowserLoginOperation {
	return BrowserLoginOperation{ProfileKey: "opaque-profile", OperationOwner: strings.Repeat("owner-fixture-", 4), ConnectionID: "opaque-connection", Generation: "generation", Revision: "revision", RecoveryClaim: "claim", Budget: BrowserLoginBudget{DeadlineAt: time.Now().Add(time.Minute), MaxBrowserAttempts: 1, MaxCredentialAttempts: 1}}
}
func browserFixtureResult(o BrowserLoginOperation, ok bool) map[string]any {
	return map[string]any{"ok": ok, "deadline_at": o.Budget.DeadlineAt, "attempts": map[string]any{"browser": 1, "credential": 1, "solver": 0, "complete": true}}
}
func browserFixtureEnvelope(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"version": "v1", "ok": true, "data": data})
}
func browserFixtureError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"version": "v1", "ok": false, "error": map[string]any{"code": code, "message": "fixture-password-code-cookie-proxy-secret"}})
}
func newBrowserFixture(t *testing.T, handler http.HandlerFunc) *BrowserLogin {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	login, err := NewBrowserLogin(BrowserLoginConfig{URL: server.URL, BearerToken: "fixture-bearer"})
	if err != nil {
		t.Fatal(err)
	}
	return login
}
func browserFixtureCapabilities(w http.ResponseWriter) {
	browserFixtureEnvelope(w, 200, map[string]any{"recovery_budget": 1, "interactive_x": 1, "profile_harvest": []string{"x"}})
}

func TestBrowserLoginStartContinueCancel(t *testing.T) {
	o := browserFixtureOperation()
	calls := 0
	cancelled := false
	login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-bearer" {
			t.Error("bearer missing")
		}
		if r.URL.Path == "/v1/capabilities" {
			browserFixtureCapabilities(w)
			return
		}
		var req browserRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			t.Error("invalid request")
		}
		got := req.BrowserLoginOperation
		got.Budget = o.Budget
		if !reflect.DeepEqual(got, o) || req.Platform != "x" {
			t.Error("operation binding changed")
		}
		if r.URL.Path == "/v1/challenges/cancel" {
			cancelled = true
			browserFixtureEnvelope(w, 200, map[string]bool{"cancelled": true})
			return
		}
		if r.URL.Path != "/v1/login/bounded" {
			t.Error("unexpected login route")
		}
		calls++
		result := browserFixtureResult(o, calls == 2)
		if calls == 1 {
			if req.Username != "fixture-user" || req.Password != "fixture-password" || req.VerificationCode != "" || req.ChallengeID != "" {
				t.Error("start payload")
			}
			result["failureType"] = "verification_required"
			result["challenge"] = BrowserLoginChallenge{ID: "challenge-reference", Method: "verification_code", MaskedDestination: "a***@example.invalid", ExpiresAt: o.Budget.DeadlineAt.Add(-time.Second)}
			browserFixtureEnvelope(w, 401, result)
			return
		}
		if req.Username != "" || req.Password != "" || req.ChallengeID != "challenge-reference" || req.VerificationCode != "fixture-code" || req.Budget.MaxBrowserAttempts != 0 || req.Budget.MaxCredentialAttempts != 0 || req.Budget.MaxSolverAttempts != 0 || !req.Budget.DeadlineAt.Equal(o.Budget.DeadlineAt) {
			t.Error("continuation may not submit credentials, launch a browser, or extend deadline")
		}
		result["session"] = Session{AuthToken: "fixture-auth", CT0: "fixture-csrf", UserAgent: "fixture-browser-UA", Twid: "u=7"}
		browserFixtureEnvelope(w, 200, result)
	})
	result, err := login.Start(context.Background(), BrowserLoginRequest{Username: "fixture-user", Password: "fixture-password", Operation: o})
	if err != nil || result.Session != nil || result.Challenge == nil {
		t.Fatalf("start: %v %v", result, err)
	}
	result, err = login.Continue(context.Background(), o, result.Challenge.ID, "fixture-code")
	if err != nil || result.Session == nil || result.Session.UserAgent != "fixture-browser-UA" || result.Attempts.Credential != 1 {
		t.Fatalf("continue: %v %v", result, err)
	}
	if err = login.Cancel(context.Background(), o); err != nil || !cancelled || calls != 2 {
		t.Fatalf("cancel: %v", err)
	}
}

func TestBrowserLoginHarvestAffinityAndNoCredentials(t *testing.T) {
	o := browserFixtureOperation()
	o.ProxyURL = "http://proxy-user:proxy-pass@proxy.invalid:8080"
	o.ProxyLease = "opaque-lease"
	login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			browserFixtureCapabilities(w)
			return
		}
		var req map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, key := range []string{"username", "password", "totp_secret", "verification_code", "challenge_id"} {
			if _, exists := req[key]; exists {
				t.Errorf("harvest sent %s", key)
			}
		}
		var budget BrowserLoginBudget
		_ = json.Unmarshal(req["budget"], &budget)
		if budget.MaxCredentialAttempts != 0 {
			t.Error("harvest password budget")
		}
		result := browserFixtureResult(o, true)
		result["attempts"] = BrowserLoginAttempts{Browser: 1, Complete: true}
		result["session"] = Session{AuthToken: "fixture-auth", CT0: "fixture-ct0", UserAgent: "fixture-UA"}
		browserFixtureEnvelope(w, 200, result)
	})
	result, err := login.Harvest(context.Background(), o)
	if err != nil || result.Session.Proxy != o.ProxyURL || result.Session.UserAgent != "fixture-UA" {
		t.Fatalf("affinity: %v %v", result, err)
	}
}

func TestBrowserLoginFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		status int
		mutate func(map[string]any)
	}{
		{"missing auth", 200, func(m map[string]any) { m["session"] = Session{CT0: "csrf", UserAgent: "UA"} }},
		{"missing UA", 200, func(m map[string]any) { m["session"] = Session{AuthToken: "auth", CT0: "csrf"} }},
		{"missing attempts", 200, func(m map[string]any) { delete(m, "attempts") }},
		{"incomplete attempts", 200, func(m map[string]any) {
			m["attempts"] = map[string]any{"complete": true, "browser": 1, "credential": 1}
		}},
		{"over budget", 200, func(m map[string]any) {
			m["attempts"] = BrowserLoginAttempts{Browser: 2, Credential: 1, Complete: true}
		}},
		{"extended deadline", 200, func(m map[string]any) { m["deadline_at"] = time.Now().Add(time.Hour) }},
		{"missing deadline", 200, func(m map[string]any) { delete(m, "deadline_at") }},
		{"conflicting session", 200, func(m map[string]any) { m["cookies"] = map[string]string{"auth_token": "different"} }},
		{"failure marked success", 200, func(m map[string]any) { m["failureType"] = "login_rejected" }},
		{"HTTP500 success", 500, func(map[string]any) {}},
		{"expired challenge", 401, func(m map[string]any) {
			m["ok"] = false
			m["failureType"] = "verification_required"
			m["challenge"] = BrowserLoginChallenge{ID: "challenge", Method: "verification_code", ExpiresAt: time.Now().Add(-time.Second)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := browserFixtureOperation()
			calls := 0
			login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/capabilities" {
					browserFixtureCapabilities(w)
					return
				}
				calls++
				m := browserFixtureResult(o, true)
				m["session"] = Session{AuthToken: "auth", CT0: "csrf", UserAgent: "UA"}
				tc.mutate(m)
				browserFixtureEnvelope(w, tc.status, m)
			})
			result, err := login.Start(context.Background(), BrowserLoginRequest{Username: "user", Password: "secret", Operation: o})
			if err == nil || (result != nil && result.Session != nil) || calls != 1 {
				t.Fatalf("accepted invalid response: %v %v calls%d", result, err, calls)
			}
		})
	}
}

func TestBrowserLoginEnvelopesAndLimits(t *testing.T) {
	for _, body := range []string{`{"version":"v2","ok":true,"data":{}}`, `{"version":"v1","ok":true}`, `{"version":"v1","ok":true,"data":null}`, `not JSON secret`, strings.Repeat("x", (1<<20)+1), `{"version":"v1","ok":true,"data":{}} trailing`} {
		login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
		_, err := login.Harvest(context.Background(), browserFixtureOperation())
		if !errors.Is(err, ErrBrowserProtocol) {
			t.Fatalf("malformed envelope: %v", err)
		}
	}
	for _, data := range []map[string]any{{"recovery_budget": 1, "profile_harvest": []string{"x"}}, {"recovery_budget": 0, "interactive_x": 1, "profile_harvest": []string{"x"}}, {"recovery_budget": 1, "interactive_x": 1}} {
		posts := 0
		login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				posts++
			}
			browserFixtureEnvelope(w, 200, data)
		})
		_, err := login.Harvest(context.Background(), browserFixtureOperation())
		if !errors.Is(err, ErrBrowserService) || posts != 0 {
			t.Fatalf("unsupported peer received login: %v", err)
		}
	}
}

func TestBrowserLoginErrorClassificationAndOwner(t *testing.T) {
	cases := []struct {
		code   string
		status int
		want   error
	}{{"operation_owner_mismatch", 409, ErrBrowserOwnerMismatch}, {"challenge_not_found", 404, ErrBrowserChallengeExpired}, {"challenge_expired", 404, ErrBrowserChallengeExpired}, {"rate_limited", 429, ErrRateLimited}, {"proxy_error", 401, ErrBrowserProxy}, {"service_unavailable", 503, ErrBrowserService}, {"login_rejected", 401, ErrUnauthorized}, {"verification_required", 401, ErrChallenge}, {"transport_error", 502, ErrRequestFailed}, {"unknown-message-secret", 401, ErrBrowserProtocol}}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/capabilities" {
					browserFixtureCapabilities(w)
					return
				}
				w.Header().Set("Retry-After", "15")
				browserFixtureError(w, tc.status, tc.code)
			})
			_, err := login.Continue(context.Background(), browserFixtureOperation(), "challenge", "code")
			if !errors.Is(err, tc.want) {
				t.Fatalf("classification: %v", err)
			}
			var typed *BrowserLoginError
			if !errors.As(err, &typed) || typed.RetryAfter != 15*time.Second || strings.Contains(fmt.Sprintf("%+v %#v", err, err), "secret") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}

func TestBrowserLoginDeadlineCancellationAndRedirect(t *testing.T) {
	calls := 0
	login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) { calls++; browserFixtureCapabilities(w) })
	o := browserFixtureOperation()
	o.Budget.DeadlineAt = time.Now().Add(-time.Second)
	if _, err := login.Continue(context.Background(), o, "challenge", "code"); !errors.Is(err, context.DeadlineExceeded) || calls != 0 {
		t.Fatalf("expired operation contacted service: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := login.Start(ctx, BrowserLoginRequest{Username: "u", Password: "p", Operation: browserFixtureOperation()}); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("cancelled operation contacted service: %v", err)
	}
	leaked := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked++ }))
	defer target.Close()
	login = newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			browserFixtureCapabilities(w)
			return
		}
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	if _, err := login.Start(context.Background(), BrowserLoginRequest{Username: "u", Password: "secret", Operation: browserFixtureOperation()}); err == nil || leaked != 0 {
		t.Fatalf("redirect leaked login: %v", err)
	}
}

func TestBrowserLoginFormattingRedactsSecrets(t *testing.T) {
	secret := "fixture-sensitive-value"
	o := browserFixtureOperation()
	o.OperationOwner = secret + secret
	o.ProxyURL = "http://" + secret + ":" + secret + "@proxy.invalid"
	o.ProfileKey = secret
	values := []any{BrowserLoginConfig{URL: secret, BearerToken: secret}, BrowserLoginRequest{Username: secret, Password: secret, Operation: o}, o, BrowserLoginChallenge{ID: secret, MaskedDestination: secret}, BrowserLoginResult{Session: &Session{AuthToken: secret, CT0: secret}, Challenge: &BrowserLoginChallenge{ID: secret}}, LoginParams{Username: secret, Password: secret, OTP: secret, TOTPSecret: secret, ProxyURL: secret}, LoginResult{Cookies: Cookies{AuthToken: secret, CT0: secret}}, &BrowserLoginError{Kind: secret}}
	for _, value := range values {
		formatted := fmt.Sprintf("%v %+v %#v", value, value, value)
		var buffer bytes.Buffer
		slog.New(slog.NewJSONHandler(&buffer, nil)).Info("fixture", "value", value)
		if strings.Contains(formatted, secret) || strings.Contains(buffer.String(), secret) {
			t.Fatalf("secret formatting leaked for %T", value)
		}
	}
	login, err := NewBrowserLogin(BrowserLoginConfig{URL: "http://sidecar.invalid", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("transport secret %s", secret) })}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = login.Harvest(context.Background(), browserFixtureOperation())
	if strings.Contains(fmt.Sprintf("%+v %#v", err, err), secret) || !errors.Is(err, ErrRequestFailed) {
		t.Fatal("transport text leaked")
	}
}

func TestBrowserLoginInvalidOperationsDoNotRequest(t *testing.T) {
	calls := 0
	login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, mutate := range []func(*BrowserLoginOperation){func(o *BrowserLoginOperation) { o.OperationOwner = "short" }, func(o *BrowserLoginOperation) { o.ProfileKey = "" }, func(o *BrowserLoginOperation) { o.Budget.DeadlineAt = time.Time{} }, func(o *BrowserLoginOperation) { o.Budget.MaxCredentialAttempts = 2 }, func(o *BrowserLoginOperation) { o.ProxyURL = "http://proxy.invalid" }, func(o *BrowserLoginOperation) { o.ProxyURL = "bad"; o.ProxyLease = "lease" }} {
		o := browserFixtureOperation()
		mutate(&o)
		_, err := login.Start(context.Background(), BrowserLoginRequest{Username: "user", Password: "password", Operation: o})
		if !errors.Is(err, ErrInvalidParams) || calls != 0 {
			t.Fatalf("invalid operation accepted: %v", err)
		}
	}
	for _, endpoint := range []string{"ftp://sidecar.invalid", "http://user:password@sidecar.invalid", "http://sidecar.invalid/prefix", "http://sidecar.invalid?secret=true", ""} {
		if _, err := NewBrowserLogin(BrowserLoginConfig{URL: endpoint}); !errors.Is(err, ErrInvalidParams) {
			t.Fatal("unsafe endpoint accepted")
		}
	}
}

func TestBrowserLoginKnownFailureReturnsWorkMetadata(t *testing.T) {
	o := browserFixtureOperation()
	login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			browserFixtureCapabilities(w)
			return
		}
		w.Header().Set("Retry-After", "40")
		w.WriteHeader(429)
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "v1", "ok": false, "error": map[string]any{"code": "rate_limited", "message": "secret", "details": browserFixtureResult(o, false)}})
	})
	result, err := login.Start(context.Background(), BrowserLoginRequest{Username: "user", Password: "password", Operation: o})
	if !errors.Is(err, ErrRateLimited) || result == nil || result.Session != nil || result.Attempts.Credential != 1 {
		t.Fatalf("missing known work: %v %v", result, err)
	}
}
func TestBrowserLoginWrongOwnerContinuation(t *testing.T) {
	original := browserFixtureOperation()
	changed := original
	changed.OperationOwner = strings.Repeat("different-owner", 3)
	login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			browserFixtureCapabilities(w)
			return
		}
		var wire browserRequest
		_ = json.NewDecoder(r.Body).Decode(&wire)
		if wire.OperationOwner != original.OperationOwner {
			browserFixtureError(w, 409, "operation_owner_mismatch")
			return
		}
		t.Error("wrong owner unexpectedly retained")
	})
	_, err := login.Continue(context.Background(), changed, "original-challenge", "code")
	if !errors.Is(err, ErrBrowserOwnerMismatch) || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong owner classification: %v", err)
	}
}
func TestBrowserLoginInFlightCancellation(t *testing.T) {
	entered := make(chan struct{}, 1)
	login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := login.Harvest(ctx, browserFixtureOperation()); finished <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel classification: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop HTTP request")
	}
}
func TestBrowserLoginCancelAfterDeadline(t *testing.T) {
	o := browserFixtureOperation()
	o.Budget.DeadlineAt = time.Now().Add(-time.Second)
	calls := 0
	login := newBrowserFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/challenges/cancel" {
			t.Error("wrong route")
		}
		browserFixtureEnvelope(w, 200, map[string]bool{"cancelled": true})
	})
	if err := login.Cancel(context.Background(), o); err != nil || calls != 1 {
		t.Fatalf("expired cancellation: %v", err)
	}
}
