package x

import (
	"context"
	"fmt"
)

// SearchOption configures SearchTweets and SearchUsers.
type SearchOption func(*searchOptions)

type searchOptions struct {
	searchType SearchType
	since      string
	until      string
}

// WithSearchType sets the search tab (Top, Latest, People, Media, Lists).
func WithSearchType(t SearchType) SearchOption {
	return func(o *searchOptions) { o.searchType = t }
}

// WithSearchSince filters results to tweets after this date (YYYY-MM-DD).
func WithSearchSince(date string) SearchOption {
	return func(o *searchOptions) { o.since = date }
}

// WithSearchUntil filters results to tweets before this date (YYYY-MM-DD).
func WithSearchUntil(date string) SearchOption {
	return func(o *searchOptions) { o.until = date }
}

// SearchTweets returns the first page of tweet search results.
func (c *Client) SearchTweets(ctx context.Context, query string, count int, opts ...SearchOption) (TweetPage, error) {
	return c.SearchTweetsPage(ctx, query, count, "", opts...)
}

// SearchTweetsPage returns a page of tweet search results starting from cursor.
func (c *Client) SearchTweetsPage(ctx context.Context, query string, count int, cursor string, opts ...SearchOption) (TweetPage, error) {
	if query == "" {
		return TweetPage{}, fmt.Errorf("%w: query must not be empty", ErrInvalidParams)
	}
	if count <= 0 {
		count = 20
	}

	so := &searchOptions{searchType: SearchTop}
	for _, o := range opts {
		o(so)
	}

	q := buildSearchQuery(query, so)

	vars := map[string]interface{}{
		"rawQuery":    q,
		"count":       count,
		"querySource": "typed_query",
		"product":     string(so.searchType),
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}

	raw, err := c.graphqlGET(ctx, "SearchTimeline", vars)
	if err != nil {
		return TweetPage{}, err
	}

	return parseTweetPage(raw, "search_by_raw_query.search_timeline")
}

// SearchUsers returns the first page of user search results.
func (c *Client) SearchUsers(ctx context.Context, query string, count int) (UserPage, error) {
	return c.searchUsersPage(ctx, query, count, "")
}

func (c *Client) searchUsersPage(ctx context.Context, query string, count int, cursor string) (UserPage, error) {
	if query == "" {
		return UserPage{}, fmt.Errorf("%w: query must not be empty", ErrInvalidParams)
	}
	if count <= 0 {
		count = 20
	}

	vars := map[string]interface{}{
		"rawQuery":    query,
		"count":       count,
		"querySource": "typed_query",
		"product":     string(SearchPeople),
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}

	raw, err := c.graphqlGET(ctx, "SearchTimeline", vars)
	if err != nil {
		return UserPage{}, err
	}

	return parseUserPage(raw, "search_by_raw_query.search_timeline")
}

func buildSearchQuery(query string, so *searchOptions) string {
	q := query
	if so.since != "" {
		q += " since:" + so.since
	}
	if so.until != "" {
		q += " until:" + so.until
	}
	return q
}
