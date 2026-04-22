package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// GetListInput is the typed input for x_get_list.
type GetListInput struct {
	ListID string `json:"list_id" jsonschema:"description=numeric X list ID,required"`
}

func getList(ctx context.Context, c *x.Client, in GetListInput) (any, error) {
	return c.GetList(ctx, in.ListID)
}

// GetListTimelineInput is the typed input for x_get_list_timeline.
type GetListTimelineInput struct {
	ListID string `json:"list_id" jsonschema:"description=numeric X list ID,required"`
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func getListTimeline(ctx context.Context, c *x.Client, in GetListTimelineInput) (any, error) {
	res, err := c.GetListTimelinePage(ctx, in.ListID, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	limit := in.Count
	if limit <= 0 {
		limit = 20
	}
	return mcptool.PageOf(res.Tweets, res.NextCursor, limit), nil
}

// GetListMembersInput is the typed input for x_get_list_members.
type GetListMembersInput struct {
	ListID string `json:"list_id" jsonschema:"description=numeric X list ID,required"`
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func getListMembers(ctx context.Context, c *x.Client, in GetListMembersInput) (any, error) {
	res, err := c.GetListMembersPage(ctx, in.ListID, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	limit := in.Count
	if limit <= 0 {
		limit = 20
	}
	return mcptool.PageOf(res.Users, res.NextCursor, limit), nil
}

var listTools = []mcptool.Tool{
	mcptool.Define[*x.Client, GetListInput](
		"x_get_list",
		"Fetch metadata for an X list by ID",
		"GetList",
		getList,
	),
	mcptool.Define[*x.Client, GetListTimelineInput](
		"x_get_list_timeline",
		"Fetch a page of tweets from an X list (cursor-paginated)",
		"GetListTimelinePage",
		getListTimeline,
	),
	mcptool.Define[*x.Client, GetListMembersInput](
		"x_get_list_members",
		"Fetch a page of members of an X list (cursor-paginated)",
		"GetListMembersPage",
		getListMembers,
	),
}
