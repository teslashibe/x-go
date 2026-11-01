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
	"GetXChatConversation":     "low-level private XChat membership read; requires host-owned account authority and per-attempt admission",
	"GetXChatConversationPage": "low-level paged XChat transport; requires observed membership, host coverage and per-attempt admission",

	// Low-level private history transport is consumed by the host's scoped inbox
	// workflow; existing first-page DM tools retain their current contracts.
	"GetConversationPage":  "low-level paged private history; requires host-owned account authority, coverage and per-attempt request admission",
	"GetConversationsPage": "first-page compatibility transport; unverified generic continuation is intentionally unavailable",
	"GetInboxPage":         "low-level folder-history transport; requires host-owned account authority and distinct update/history coverage tracking",

	// Internal observability / lifecycle helpers — surfaced via the host
	// application's MCP middleware or initialization, not as callable tools.
	"RateLimit":                "internal observability; surfaced via the host application's MCP middleware, not as a callable tool",
	"TransactionInitErr":       "internal observability of client bootstrap state; reported at construction, not as a callable tool",
	"RefreshQueryIDs":          "internal session-bootstrap helper; managed by the client lifecycle, not exposed as an agent-callable tool",
	"TransactionReady":         "internal bounded diagnostic fact; surfaced only through operator health tooling",
	"QueryMetadataRefreshed":   "internal bounded diagnostic fact; surfaced only through operator health tooling",
	"DiagnoseSearchCapability": "read-only operator diagnostic; not an agent-callable product action",
	"MaxTweetLength":           "account-tier helper used by compose validation; agents learn the limit from tool schema / compose errors, not a separate tool",

	// UploadMedia is the general entry point wrapped by x_upload_media;
	// UploadMediaFromURL is a convenience wrapper it dispatches to.
	"UploadMediaFromURL": "convenience wrapper around UploadMedia; the x_upload_media tool accepts image_url and calls this internally",
	"UploadVideoFromURL": "convenience wrapper around UploadVideo; the x_upload_video tool accepts video_url and calls this internally",

	// Convenience wrappers around the *Page variants — the MCP tool wraps
	// the cursor-aware *Page method so a single tool covers both first-page
	// and pagination use cases.
	"HomeTimeline":         "convenience wrapper around HomeTimelinePage; the x_home_timeline tool wraps the cursor-aware variant",
	"HomeLatestTimeline":   "convenience wrapper around HomeLatestTimelinePage; the x_home_latest_timeline tool wraps the cursor-aware variant",
	"UserTweets":           "convenience wrapper around UserTweetsPage; the x_get_user_tweets tool wraps the cursor-aware variant",
	"SearchTweets":         "convenience wrapper around SearchTweetsPage; the x_search_tweets tool wraps the cursor-aware variant",
	"SearchUsers":          "convenience wrapper around SearchUsersPage; the x_search_users tool wraps the cursor-aware variant",
	"AdvancedSearchTweets": "convenience wrapper around AdvancedSearchTweetsPage; the x_advanced_search_tweets tool wraps the cursor-aware variant",
	"ReplyToPost":          "typed-outcome wrapper used by the existing reply MCP tool",
	"CreatePost":           "typed-outcome wrapper used by the existing x_create_tweet MCP tool",
	"QuotePost":            "typed-outcome wrapper used by the existing x_quote_tweet MCP tool",
	"GetFollowers":         "convenience wrapper around GetFollowersPage; the x_get_followers tool wraps the cursor-aware variant",
	"GetFollowing":         "convenience wrapper around GetFollowingPage; the x_get_following tool wraps the cursor-aware variant",
	"GetListTimeline":      "convenience wrapper around GetListTimelinePage; the x_get_list_timeline tool wraps the cursor-aware variant",
	"GetListMembers":       "convenience wrapper around GetListMembersPage; the x_get_list_members tool wraps the cursor-aware variant",

	// Typed write outcomes and state reads for the host's action executor.
	// They need host-owned account authority, admission, approval and
	// reconcile; the existing x_like, x_follow, x_delete_tweet (and sibling)
	// tools keep their contracts over the untyped methods.
	"LikePost":            "typed-outcome write for the host's action executor; the existing x_like tool keeps its contract",
	"UnlikePost":          "typed-outcome write for the host's action executor; the existing x_unlike tool keeps its contract",
	"Repost":              "typed-outcome write for the host's action executor; the existing x_retweet tool keeps its contract",
	"Unrepost":            "typed-outcome write for the host's action executor; the existing x_unretweet tool keeps its contract",
	"BookmarkPost":        "typed-outcome write for the host's action executor; the existing x_bookmark tool keeps its contract",
	"UnbookmarkPost":      "typed-outcome write for the host's action executor; the existing x_unbookmark tool keeps its contract",
	"DeletePost":          "typed-outcome write for the host's action executor; the existing x_delete_tweet tool keeps its contract",
	"FollowUser":          "typed-outcome write for the host's action executor; the existing x_follow tool keeps its contract",
	"UnfollowUser":        "typed-outcome write for the host's action executor; the existing x_unfollow tool keeps its contract",
	"MuteUser":            "typed-outcome write for the host's action executor; the existing x_mute tool keeps its contract",
	"UnmuteUser":          "typed-outcome write for the host's action executor; the existing x_unmute tool keeps its contract",
	"BlockUser":           "typed-outcome write for the host's action executor; the existing x_block tool keeps its contract",
	"UnblockUser":         "typed-outcome write for the host's action executor; the existing x_unblock tool keeps its contract",
	"GetPostState":        "single-request post state read used by the host's action reconcile; not an agent-callable tool",
	"GetUserRelationship": "single-request relationship read used by the host's action reconcile; not an agent-callable tool",
}
