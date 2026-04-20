package x

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	baseURL          = "https://x.com"
	graphqlBase      = "https://x.com/i/api/graphql"
	bearerToken      = "AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"
	defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36"
	defaultMinGap    = 1 * time.Second
	defaultMaxRetries = 3
	defaultRetryBase  = 500 * time.Millisecond
)

// Default queryIDs harvested from X's main.js bundle.
// These rotate with each deploy; override via WithQueryIDs.
var defaultQueryIDs = map[string]string{
	"HomeTimeline":             "L8Lb9oomccM012S7fQ-QKA",
	"HomeLatestTimeline":       "tzmrSIWxyV4IRRh9nij6TQ",
	"SearchTimeline":           "rkp6b4vtR9u7v3naGoOzUQ",
	"UserByScreenName":         "IGgvgiOx4QZndDHuD3x9TQ",
	"UserByRestId":             "VQfQ9wwYdk6j_u2O4vt64Q",
	"UserTweets":               "O0epvwaQPUx-bT9YlqlL6w",
	"TweetDetail":              "xIYgDwjboktoFeXe_fgacw",
	"TweetResultByRestId":      "zy39CwTyYhU-_0LP7dljjg",
	"Followers":                "Enf9DNUZYiT037aersI5gg",
	"Following":                "ntIPnH1WMBKW--4Tn1q71A",
	"FollowersYouKnow":         "VkDQMmwC1VJjoUVwuYVepA",
	"ListBySlug":               "LDQpQ89B5ipR8izCKrWU0g",
	"ListLatestTweetsTimeline":  "fb_6wmHD2dk9D-xYXOQlgw",
	"ListMembers":              "oZLcyjKOfXBf2Jln31YXPw",
	"ListMemberships":          "en6N7nVkbafxIMQa8ef2DA",
	"Viewer":                   "k3YiLNE_MAy5J-NANLERUg",
}

// defaultFeatures is the standard features map sent with every GraphQL request.
var defaultFeatures = map[string]bool{
	"rweb_tipjar_consumption_enabled":                                         true,
	"responsive_web_graphql_exclude_directive_enabled":                        true,
	"verified_phone_label_enabled":                                            false,
	"creator_subscriptions_tweet_preview_api_enabled":                         true,
	"responsive_web_graphql_timeline_navigation_enabled":                      true,
	"responsive_web_graphql_skip_user_profile_image_extensions_enabled":       false,
	"communities_web_enable_tweet_community_results_fetch":                    true,
	"c9s_tweet_anatomy_moderator_badge_enabled":                               true,
	"articles_preview_enabled":                                                true,
	"responsive_web_edit_tweet_api_enabled":                                   true,
	"graphql_is_translatable_rweb_tweet_is_translatable_enabled":              true,
	"view_counts_everywhere_api_enabled":                                      true,
	"longform_notetweets_consumption_enabled":                                 true,
	"responsive_web_twitter_article_tweet_consumption_enabled":                true,
	"tweet_awards_web_tipping_enabled":                                        false,
	"creator_subscriptions_quote_tweet_preview_enabled":                       false,
	"freedom_of_speech_not_reach_fetch_enabled":                               true,
	"standardized_nudges_misinfo":                                             true,
	"tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled": true,
	"rweb_video_timestamps_enabled":                                           true,
	"longform_notetweets_rich_text_read_enabled":                              true,
	"longform_notetweets_inline_media_enabled":                                true,
	"responsive_web_enhance_cards_enabled":                                    false,
}

// Client is an X API client. It is safe for concurrent use.
type Client struct {
	cookies    Cookies
	restID     string
	httpClient *http.Client
	userAgent  string
	queryIDs   map[string]string
	features   map[string]bool
	maxRetries int
	retryBase  time.Duration
	minGap     time.Duration
	gapMu      sync.Mutex
	lastReqAt  time.Time
	reqMu      sync.RWMutex // protects queryIDs
	viewer     *User
}

// Option configures a Client.
type Option func(*Client)

// WithUserAgent overrides the default Chrome User-Agent string.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithQueryIDs overrides one or more GraphQL queryIds.
func WithQueryIDs(overrides map[string]string) Option {
	return func(c *Client) {
		for k, v := range overrides {
			c.queryIDs[k] = v
		}
	}
}

// WithFeatures overrides one or more GraphQL feature flags.
func WithFeatures(overrides map[string]bool) Option {
	return func(c *Client) {
		for k, v := range overrides {
			c.features[k] = v
		}
	}
}

// WithRetry configures retry behaviour.
// Default: 3 attempts, 500ms exponential base.
func WithRetry(maxAttempts int, base time.Duration) Option {
	return func(c *Client) {
		c.maxRetries = maxAttempts
		c.retryBase = base
	}
}

// WithHTTPClient replaces the default http.Client. Nil is ignored.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithProxy routes all HTTP traffic through the given proxy URL.
func WithProxy(proxyURL string) Option {
	return func(c *Client) {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(parsed)
		c.httpClient = &http.Client{
			Timeout:   c.httpClient.Timeout,
			Transport: transport,
		}
	}
}

// WithMinRequestGap sets the minimum time between consecutive requests.
// Default: 1s. Lower values risk triggering X's rate limiter.
func WithMinRequestGap(d time.Duration) Option {
	return func(c *Client) { c.minGap = d }
}

// New creates a Client and validates the session via the Viewer query.
// Returns ErrInvalidAuth if AuthToken or CT0 is empty.
func New(cookies Cookies, opts ...Option) (*Client, error) {
	if cookies.AuthToken == "" || cookies.CT0 == "" {
		return nil, fmt.Errorf("%w: AuthToken and CT0 must both be non-empty", ErrInvalidAuth)
	}

	restID := parseRestID(cookies.Twid)

	qids := make(map[string]string, len(defaultQueryIDs))
	for k, v := range defaultQueryIDs {
		qids[k] = v
	}

	feats := make(map[string]bool, len(defaultFeatures))
	for k, v := range defaultFeatures {
		feats[k] = v
	}

	c := &Client{
		cookies:    cookies,
		restID:     restID,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  defaultUserAgent,
		queryIDs:   qids,
		features:   feats,
		maxRetries: defaultMaxRetries,
		retryBase:  defaultRetryBase,
		minGap:     defaultMinGap,
	}

	for _, o := range opts {
		o(c)
	}

	if err := c.validateSession(context.Background()); err != nil {
		return nil, err
	}
	return c, nil
}

// Me returns the authenticated user's profile.
func (c *Client) Me(ctx context.Context) (*User, error) {
	if c.viewer != nil {
		return c.viewer, nil
	}
	return nil, ErrUnauthorized
}

// parseRestID extracts the numeric user ID from the twid cookie value.
// The twid cookie is URL-encoded and has the format "u=<restId>".
func parseRestID(twid string) string {
	if twid == "" {
		return ""
	}
	decoded, err := url.QueryUnescape(twid)
	if err != nil {
		decoded = twid
	}
	if strings.HasPrefix(decoded, "u=") {
		return decoded[2:]
	}
	return decoded
}
