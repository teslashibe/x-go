package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// HomeTimelineInput is the typed input for x_home_timeline.
type HomeTimelineInput struct {
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func homeTimeline(ctx context.Context, c *x.Client, in HomeTimelineInput) (any, error) {
	res, err := c.HomeTimelinePage(ctx, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	limit := in.Count
	if limit <= 0 {
		limit = 20
	}
	return mcptool.PageOf(res.Tweets, res.NextCursor, limit), nil
}

// HomeLatestTimelineInput is the typed input for x_home_latest_timeline.
type HomeLatestTimelineInput struct {
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func homeLatestTimeline(ctx context.Context, c *x.Client, in HomeLatestTimelineInput) (any, error) {
	res, err := c.HomeLatestTimelinePage(ctx, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	limit := in.Count
	if limit <= 0 {
		limit = 20
	}
	return mcptool.PageOf(res.Tweets, res.NextCursor, limit), nil
}

var timelineTools = []mcptool.Tool{
	mcptool.Define[*x.Client, HomeTimelineInput](
		"x_home_timeline",
		"Fetch a page of the algorithmic 'For You' home timeline (cursor-paginated)",
		"HomeTimelinePage",
		homeTimeline,
	),
	mcptool.Define[*x.Client, HomeLatestTimelineInput](
		"x_home_latest_timeline",
		"Fetch a page of the reverse-chronological 'Following' home timeline (cursor-paginated)",
		"HomeLatestTimelinePage",
		homeLatestTimeline,
	),
}
