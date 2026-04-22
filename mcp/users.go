package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// GetProfileInput is the typed input for x_get_profile.
type GetProfileInput struct {
	ScreenName string `json:"screen_name" jsonschema:"description=X handle without the @ (e.g. 'elonmusk'),required"`
}

func getProfile(ctx context.Context, c *x.Client, in GetProfileInput) (any, error) {
	return c.GetProfile(ctx, in.ScreenName)
}

// GetProfileByIDInput is the typed input for x_get_profile_by_id.
type GetProfileByIDInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric X user ID (rest_id),required"`
}

func getProfileByID(ctx context.Context, c *x.Client, in GetProfileByIDInput) (any, error) {
	return c.GetProfileByID(ctx, in.UserID)
}

// MeInput is the typed input for x_me.
type MeInput struct{}

func me(ctx context.Context, c *x.Client, _ MeInput) (any, error) {
	return c.Me(ctx)
}

// GetFollowersInput is the typed input for x_get_followers.
type GetFollowersInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric X user ID whose followers to fetch,required"`
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func getFollowers(ctx context.Context, c *x.Client, in GetFollowersInput) (any, error) {
	res, err := c.GetFollowersPage(ctx, in.UserID, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	limit := in.Count
	if limit <= 0 {
		limit = 20
	}
	return mcptool.PageOf(res.Users, res.NextCursor, limit), nil
}

// GetFollowingInput is the typed input for x_get_following.
type GetFollowingInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric X user ID whose following list to fetch,required"`
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func getFollowing(ctx context.Context, c *x.Client, in GetFollowingInput) (any, error) {
	res, err := c.GetFollowingPage(ctx, in.UserID, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	limit := in.Count
	if limit <= 0 {
		limit = 20
	}
	return mcptool.PageOf(res.Users, res.NextCursor, limit), nil
}

var userTools = []mcptool.Tool{
	mcptool.Define[*x.Client, GetProfileInput](
		"x_get_profile",
		"Fetch an X user profile by handle (the @ name)",
		"GetProfile",
		getProfile,
	),
	mcptool.Define[*x.Client, GetProfileByIDInput](
		"x_get_profile_by_id",
		"Fetch an X user profile by numeric user ID (rest_id)",
		"GetProfileByID",
		getProfileByID,
	),
	mcptool.Define[*x.Client, MeInput](
		"x_me",
		"Return the authenticated X user's own profile",
		"Me",
		me,
	),
	mcptool.Define[*x.Client, GetFollowersInput](
		"x_get_followers",
		"Fetch a page of followers of an X user (cursor-paginated)",
		"GetFollowersPage",
		getFollowers,
	),
	mcptool.Define[*x.Client, GetFollowingInput](
		"x_get_following",
		"Fetch a page of users an X user is following (cursor-paginated)",
		"GetFollowingPage",
		getFollowing,
	),
}
