package x

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetList retrieves metadata for an X list by its ID.
func (c *Client) GetList(ctx context.Context, listID string) (*List, error) {
	if listID == "" {
		return nil, fmt.Errorf("%w: listID must not be empty", ErrInvalidParams)
	}

	vars := map[string]interface{}{
		"listId": listID,
	}

	raw, err := c.graphqlGET(ctx, "ListBySlug", vars)
	if err != nil {
		return nil, err
	}

	var data struct {
		List listObj `json:"list"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("%w: decoding list: %v", ErrRequestFailed, err)
	}

	if data.List.Name == "" {
		return nil, ErrNotFound
	}

	l := toList(data.List)
	return &l, nil
}

// GetListTimeline returns the first page of tweets from a list.
func (c *Client) GetListTimeline(ctx context.Context, listID string, count int) (TweetPage, error) {
	return c.GetListTimelinePage(ctx, listID, count, "")
}

// GetListTimelinePage returns a page of tweets from a list starting from cursor.
// Coverage reports stalled pages and partial errors (see PageEnd).
func (c *Client) GetListTimelinePage(ctx context.Context, listID string, count int, cursor string) (TweetPage, error) {
	if listID == "" {
		return TweetPage{}, fmt.Errorf("%w: listID must not be empty", ErrInvalidParams)
	}
	if count <= 0 {
		count = 20
	}

	vars := map[string]interface{}{
		"listId": listID,
		"count":  count,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}

	ctx, req := newPageRequest(ctx, "ListLatestTweetsTimeline", "list.tweets_timeline", cursor)
	raw, err := c.graphqlGET(ctx, "ListLatestTweetsTimeline", vars)
	if err != nil {
		return TweetPage{}, err
	}

	return req.tweetPage(raw)
}

// GetListMembers returns the first page of members of a list.
func (c *Client) GetListMembers(ctx context.Context, listID string, count int) (UserPage, error) {
	return c.GetListMembersPage(ctx, listID, count, "")
}

// GetListMembersPage returns a page of list members starting from cursor.
// Coverage reports stalled pages and partial errors (see PageEnd).
func (c *Client) GetListMembersPage(ctx context.Context, listID string, count int, cursor string) (UserPage, error) {
	if listID == "" {
		return UserPage{}, fmt.Errorf("%w: listID must not be empty", ErrInvalidParams)
	}
	if count <= 0 {
		count = 20
	}

	vars := map[string]interface{}{
		"listId": listID,
		"count":  count,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}

	ctx, req := newPageRequest(ctx, "ListMembers", "list.members_timeline", cursor)
	raw, err := c.graphqlGET(ctx, "ListMembers", vars)
	if err != nil {
		return UserPage{}, err
	}

	return req.userPage(raw)
}
