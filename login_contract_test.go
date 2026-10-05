package x

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLegacyLoginOTPUserAgentAndStatus(t *testing.T) {
	for _, status := range []int{200, 401, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/login" {
					t.Error("legacy changed protocol")
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["verificationCode"] != "fixture-otp" || body["totpSecret"] != nil {
					t.Error("OTP must not be an authenticator seed")
				}
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "cookies": map[string]string{"auth_token": "auth", "ct0": "csrf"}, "session": Session{UserAgent: "browser-UA"}})
			}))
			defer server.Close()
			result, err := Login(context.Background(), LoginParams{Username: "user", Password: "password", OTP: "fixture-otp", UserAgent: "caller-UA", SidecarURL: server.URL})
			if requests != 1 {
				t.Error("login retried")
			}
			if status == 200 {
				if err != nil || result.UserAgent != "browser-UA" {
					t.Fatalf("legacy: %v %v", result, err)
				}
			} else if err == nil || result != nil {
				t.Error("HTTP failure became success")
			}
		})
	}
}

func TestLegacyLoginRedirectDoesNotForwardCredentials(t *testing.T) {
	leaked := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked++ }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(307)
	}))
	defer server.Close()
	_, err := Login(context.Background(), LoginParams{Username: "user", Password: "password", SidecarURL: server.URL})
	if err == nil || leaked != 0 {
		t.Fatalf("redirect followed: %v", err)
	}
}

func TestClassicLoginInteractiveChallenge(t *testing.T) {
	flow := loginFlow{params: LoginParams{OTP: "fixture-code"}}
	raw, done, err := flow.respond(context.Background(), []subtask{{SubtaskID: "LoginAcid"}})
	if err != nil || done || !strings.Contains(string(raw), "fixture-code") {
		t.Fatalf("LoginAcid: %v", err)
	}
	flow.params.OTP = ""
	if _, _, err = flow.respond(context.Background(), []subtask{{SubtaskID: "LoginAcid"}}); !errors.Is(err, ErrChallenge) {
		t.Fatal("missing code must challenge")
	}
	if _, done, err = flow.respond(context.Background(), []subtask{{SubtaskID: "FutureVerificationSubtask"}}); done || !errors.Is(err, ErrChallenge) {
		t.Fatal("unknown subtask became success")
	}
}

func fixtureViewerClient(rt http.RoundTripper) *Client {
	c := newTestClient(rt)
	c.queryIDs = map[string]string{"Viewer": "fixture-viewer", "UserByRestId": "fixture-profile"}
	c.maxRetries = 1
	return c
}
func TestViewerFailuresPreserveClassification(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		transport bool
		want      error
	}{
		{"HTTP429", 429, `{"errors":[{"code":88,"message":"rate limit"}]}`, false, ErrRateLimited},
		{"provider88", 200, `{"errors":[{"code":88,"message":"rate limit"}]}`, false, ErrRateLimited},
		{"challenge", 200, `{"errors":[{"message":"verification required"}]}`, false, ErrChallenge},
		{"transport", 0, "", true, ErrRequestFailed},
		{"malformed", 200, `{"data":{"viewer":{"user_results":{"result":{}}}}}`, false, ErrRequestFailed},
		{"outage", 503, `unavailable`, false, ErrRequestFailed},
		{"unauthorized", 401, `{}`, false, ErrUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureViewerClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
				if tc.transport {
					return nil, errors.New("offline")
				}
				return jsonResponse(tc.status, tc.body), nil
			}))
			err := c.validateSession(context.Background())
			if !errors.Is(err, tc.want) || (tc.want != ErrUnauthorized && errors.Is(err, ErrUnauthorized)) {
				t.Fatalf("classification: %v", err)
			}
		})
	}
}
func TestChallengeReadDoesNotRetry(t *testing.T) {
	calls := 0
	c := fixtureViewerClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(200, `{"errors":[{"message":"verify your identity"}]}`), nil
	}))
	c.maxRetries = 4
	c.retryBase = time.Millisecond
	_, err := c.graphqlGET(context.Background(), "Viewer", nil)
	if !errors.Is(err, ErrChallenge) || calls != 1 {
		t.Fatalf("challenge attempts %d error %v", calls, err)
	}
}
func TestReadRateLimitKeepsWaitAndCooldown(t *testing.T) {
	for _, status := range []int{200, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := fixtureViewerClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
				resp := jsonResponse(status, `{"errors":[{"code":88,"message":"rate limit"}]}`)
				resp.Header.Set("Retry-After", "20")
				return resp, nil
			}))
			_, err := c.graphqlGET(context.Background(), "Viewer", nil)
			var limited *RateLimitError
			if !errors.As(err, &limited) || limited.Wait != 20*time.Second || c.rlState.Remaining != 0 || c.rlState.RetryAfter != 20*time.Second {
				t.Fatalf("cooldown: %v", err)
			}
		})
	}
}
func TestWriteStillDoesNotRetry(t *testing.T) {
	calls := 0
	c := fixtureViewerClient(roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("offline transport") }))
	c.maxRetries = 4
	_, err := c.graphqlPOST(context.Background(), "Viewer", nil)
	if err == nil || calls != 1 {
		t.Fatalf("write attempts %d error %v", calls, err)
	}
}
func TestSessionNewClientUsesBrowserUserAgent(t *testing.T) {
	calls := 0
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("User-Agent") != "fixture-browser-UA" {
			t.Error("browser UA discarded")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/Viewer"):
			return jsonResponse(200, `{"data":{"viewer":{"user_results":{"result":{"rest_id":"7"}}}}}`), nil
		case strings.HasSuffix(r.URL.Path, "/UserByRestId"):
			return jsonResponse(200, `{"data":{"user":{"result":{"rest_id":"7","legacy":{"screen_name":"fixture-user"}}}}}`), nil
		default:
			return jsonResponse(503, `{}`), nil
		}
	})}
	c, err := (Session{AuthToken: "auth", CT0: "csrf", UserAgent: "fixture-browser-UA", Twid: "u=7"}).NewClient(context.Background(), WithHTTPClient(hc), WithMinRequestGap(0), WithRetry(1, 0))
	if err != nil || c.userAgent != "fixture-browser-UA" || c.viewer.ID != "7" || calls < 2 {
		t.Fatalf("NewClient: %v", err)
	}
	if c.TransactionReady() || c.TransactionInitErr() == nil {
		t.Error("bootstrap failure concealed")
	}
}
