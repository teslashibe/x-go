package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// coveredPage is a tool page that also carries the provider page's coverage,
// so an agent can tell a verified end from a missing continuation.
type coveredPage[T any] struct {
	mcptool.Page[T]
	Coverage x.PageCoverage `json:"coverage"`
}

func covered[T any](items []T, nextCursor string, limit int, cov x.PageCoverage) coveredPage[T] {
	return coveredPage[T]{Page: mcptool.PageOf(items, nextCursor, limit), Coverage: cov}
}

func pageLimit(count int) int {
	if count <= 0 {
		return 20
	}
	return count
}

// coveredTweetPage projects a tweet page like the timeline tools do and adds
// its coverage.
func coveredTweetPage(page x.TweetPage, count int, view string) (any, error) {
	projected, err := projectTweetPage(page.Tweets, page.NextCursor, pageLimit(count), view)
	if err != nil {
		return nil, err
	}
	switch p := projected.(type) {
	case mcptool.Page[x.Tweet]:
		return coveredPage[x.Tweet]{Page: p, Coverage: page.Coverage}, nil
	case mcptool.Page[tweetMetricsView]:
		return coveredPage[tweetMetricsView]{Page: p, Coverage: page.Coverage}, nil
	case mcptool.Page[tweetCompactView]:
		return coveredPage[tweetCompactView]{Page: p, Coverage: page.Coverage}, nil
	default:
		return projected, nil
	}
}

// GetBookmarksInput is the typed input for x_get_bookmarks.
type GetBookmarksInput struct {
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
	View   string `json:"view,omitempty" jsonschema:"description=response view; allowed: full,compact,metrics,default=full"`
}

func getBookmarks(ctx context.Context, c *x.Client, in GetBookmarksInput) (any, error) {
	res, err := c.GetBookmarksPage(ctx, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	return coveredTweetPage(res, in.Count, in.View)
}

// GetLikedPostsInput is the typed input for x_get_liked_posts.
type GetLikedPostsInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric X user ID whose likes to fetch (X shows likes only for the authenticated account),required"`
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
	View   string `json:"view,omitempty" jsonschema:"description=response view; allowed: full,compact,metrics,default=full"`
}

func getLikedPosts(ctx context.Context, c *x.Client, in GetLikedPostsInput) (any, error) {
	res, err := c.GetLikedPostsPage(ctx, in.UserID, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	return coveredTweetPage(res, in.Count, in.View)
}

// GetMutedUsersInput is the typed input for x_get_muted_users.
type GetMutedUsersInput struct {
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func getMutedUsers(ctx context.Context, c *x.Client, in GetMutedUsersInput) (any, error) {
	res, err := c.GetMutedUsersPage(ctx, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	return covered(res.Users, res.NextCursor, pageLimit(in.Count), res.Coverage), nil
}

// GetBlockedUsersInput is the typed input for x_get_blocked_users.
type GetBlockedUsersInput struct {
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func getBlockedUsers(ctx context.Context, c *x.Client, in GetBlockedUsersInput) (any, error) {
	res, err := c.GetBlockedUsersPage(ctx, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	return covered(res.Users, res.NextCursor, pageLimit(in.Count), res.Coverage), nil
}

// GetOwnedListsInput is the typed input for x_get_owned_lists.
type GetOwnedListsInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric X user ID whose owned lists to fetch,required"`
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func getOwnedLists(ctx context.Context, c *x.Client, in GetOwnedListsInput) (any, error) {
	res, err := c.GetOwnedListsPage(ctx, in.UserID, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	return covered(res.Lists, res.NextCursor, pageLimit(in.Count), res.Coverage), nil
}

// GetSubscribedListsInput is the typed input for x_get_subscribed_lists.
type GetSubscribedListsInput struct {
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func getSubscribedLists(ctx context.Context, c *x.Client, in GetSubscribedListsInput) (any, error) {
	res, err := c.GetSubscribedListsPage(ctx, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	return covered(res.Lists, res.NextCursor, pageLimit(in.Count), res.Coverage), nil
}

// collectionTools are the account collection reads. They follow the list
// tools and come last so the order of the older tools is unchanged.
var collectionTools = []mcptool.Tool{
	mcptool.Define[*x.Client, GetBookmarksInput](
		"x_get_bookmarks",
		"Fetch a page of the authenticated account's bookmarked posts (cursor-paginated, with coverage)",
		"GetBookmarksPage",
		getBookmarks,
	),
	mcptool.Define[*x.Client, GetLikedPostsInput](
		"x_get_liked_posts",
		"Fetch a page of posts an X user liked (cursor-paginated, with coverage)",
		"GetLikedPostsPage",
		getLikedPosts,
	),
	mcptool.Define[*x.Client, GetMutedUsersInput](
		"x_get_muted_users",
		"Fetch a page of accounts the authenticated account mutes (cursor-paginated, with coverage)",
		"GetMutedUsersPage",
		getMutedUsers,
	),
	mcptool.Define[*x.Client, GetBlockedUsersInput](
		"x_get_blocked_users",
		"Fetch a page of accounts the authenticated account blocks (cursor-paginated, with coverage)",
		"GetBlockedUsersPage",
		getBlockedUsers,
	),
	mcptool.Define[*x.Client, GetOwnedListsInput](
		"x_get_owned_lists",
		"Fetch a page of X lists a user owns (cursor-paginated, with coverage)",
		"GetOwnedListsPage",
		getOwnedLists,
	),
	mcptool.Define[*x.Client, GetSubscribedListsInput](
		"x_get_subscribed_lists",
		"Fetch a page of X lists the authenticated account subscribes to (cursor-paginated, with coverage)",
		"GetSubscribedListsPage",
		getSubscribedLists,
	),
}
