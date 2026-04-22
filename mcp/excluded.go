package mcp

// Excluded enumerates exported methods on *x.Client that are intentionally
// not exposed via MCP. Each entry must have a non-empty reason.
//
// The coverage test in mcp_test.go fails if any exported method on *Client
// is neither wrapped by a Tool nor present in this map (or vice-versa: if
// an entry here doesn't correspond to a real method).
//
// When the underlying client gains a new method:
//   - prefer to add an MCP tool for it (see tweets.go / search.go / etc.)
//   - if the method is unsuitable for an agent (internal observability,
//     auth-only helper, convenience wrapper around a more general method,
//     etc.), add it here with a reason
var Excluded = map[string]string{
	// Internal observability / lifecycle helpers — surfaced via the host
	// application's MCP middleware or initialization, not as callable tools.
	"RateLimit":          "internal observability; surfaced via the host application's MCP middleware, not as a callable tool",
	"TransactionInitErr": "internal observability of client bootstrap state; reported at construction, not as a callable tool",
	"RefreshQueryIDs":    "internal session-bootstrap helper; managed by the client lifecycle, not exposed as an agent-callable tool",

	// Convenience wrappers around the *Page variants — the MCP tool wraps
	// the cursor-aware *Page method so a single tool covers both first-page
	// and pagination use cases.
	"HomeTimeline":         "convenience wrapper around HomeTimelinePage; the x_home_timeline tool wraps the cursor-aware variant",
	"HomeLatestTimeline":   "convenience wrapper around HomeLatestTimelinePage; the x_home_latest_timeline tool wraps the cursor-aware variant",
	"UserTweets":           "convenience wrapper around UserTweetsPage; the x_get_user_tweets tool wraps the cursor-aware variant",
	"SearchTweets":         "convenience wrapper around SearchTweetsPage; the x_search_tweets tool wraps the cursor-aware variant",
	"SearchUsers":          "convenience wrapper around SearchUsersPage; the x_search_users tool wraps the cursor-aware variant",
	"AdvancedSearchTweets": "convenience wrapper around AdvancedSearchTweetsPage; the x_advanced_search_tweets tool wraps the cursor-aware variant",
	"GetFollowers":         "convenience wrapper around GetFollowersPage; the x_get_followers tool wraps the cursor-aware variant",
	"GetFollowing":         "convenience wrapper around GetFollowingPage; the x_get_following tool wraps the cursor-aware variant",
	"GetListTimeline":      "convenience wrapper around GetListTimelinePage; the x_get_list_timeline tool wraps the cursor-aware variant",
	"GetListMembers":       "convenience wrapper around GetListMembersPage; the x_get_list_members tool wraps the cursor-aware variant",
}
