package x

import "context"

// HomeTimeline returns the first page of the algorithmic home timeline.
func (c *Client) HomeTimeline(ctx context.Context, count int) (TweetPage, error) {
	return c.HomeTimelinePage(ctx, count, "")
}

// HomeTimelinePage returns a page of the algorithmic home timeline starting from cursor.
// Coverage reports stalled pages and partial errors (see PageEnd).
func (c *Client) HomeTimelinePage(ctx context.Context, count int, cursor string) (TweetPage, error) {
	if count <= 0 {
		count = 20
	}

	vars := map[string]interface{}{
		"count":                                  count,
		"includePromotedContent":                 false,
		"latestControlAvailable":                 true,
		"requestContext":                         "launch",
		"withCommunity":                          true,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}

	ctx, req := newPageRequest(ctx, "HomeTimeline", "home.home_timeline_urt", cursor)
	raw, err := c.graphqlGET(ctx, "HomeTimeline", vars)
	if err != nil {
		return TweetPage{}, err
	}

	return req.tweetPage(raw)
}

// HomeLatestTimeline returns the first page of the reverse-chronological timeline.
func (c *Client) HomeLatestTimeline(ctx context.Context, count int) (TweetPage, error) {
	return c.HomeLatestTimelinePage(ctx, count, "")
}

// HomeLatestTimelinePage returns a page of the reverse-chronological timeline starting from cursor.
// Coverage reports stalled pages and partial errors (see PageEnd).
func (c *Client) HomeLatestTimelinePage(ctx context.Context, count int, cursor string) (TweetPage, error) {
	if count <= 0 {
		count = 20
	}

	vars := map[string]interface{}{
		"count":                                  count,
		"includePromotedContent":                 false,
		"latestControlAvailable":                 true,
		"requestContext":                         "launch",
		"withCommunity":                          true,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}

	ctx, req := newPageRequest(ctx, "HomeLatestTimeline", "home.home_timeline_urt", cursor)
	raw, err := c.graphqlGET(ctx, "HomeLatestTimeline", vars)
	if err != nil {
		return TweetPage{}, err
	}

	return req.tweetPage(raw)
}
