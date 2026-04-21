package x

import (
	"context"
	"fmt"
	"strings"
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

// ReplyFilter controls which reply types are included in search results.
type ReplyFilter string

const (
	ReplyFilterInclude  ReplyFilter = ""              // include replies and original posts (default)
	ReplyFilterExclude  ReplyFilter = "exclude:replies" // only original posts
	ReplyFilterOnly     ReplyFilter = "filter:replies"  // only replies
)

// LinkFilter controls which link types are included in search results.
type LinkFilter string

const (
	LinkFilterInclude LinkFilter = ""             // include all posts (default)
	LinkFilterExclude LinkFilter = "exclude:links" // exclude posts with links
	LinkFilterOnly    LinkFilter = "filter:links"   // only posts with links
)

// AdvancedSearch mirrors X's Advanced Search UI. All fields are optional.
// Build with NewAdvancedSearch and chain setter methods, then call
// client.AdvancedSearchTweets or compile to a raw query with Build().
type AdvancedSearch struct {
	// Words
	AllWords    string   // all of these words (space-separated, ANDed)
	ExactPhrase string   // this exact phrase (quoted)
	AnyWords    []string // any of these words (ORed)
	NoneWords   []string // none of these words (excluded with -)
	Hashtags    []string // hashtags (without #)

	// Language
	Language string // BCP-47 lang code: en, es, ja, etc.

	// Accounts
	From      []string // from these accounts (without @)
	To        []string // sent in reply to these accounts
	Mentioning []string // mentioning these accounts

	// Filters
	Replies ReplyFilter
	Links   LinkFilter

	// Engagement minimums
	MinReplies  int
	MinLikes    int
	MinReposts  int

	// Dates (YYYY-MM-DD)
	Since string
	Until string

	// Search tab
	ResultType SearchType
}

// NewAdvancedSearch creates an AdvancedSearch with defaults (Top results, no filters).
func NewAdvancedSearch() *AdvancedSearch {
	return &AdvancedSearch{ResultType: SearchTop}
}

// Build compiles the AdvancedSearch into X's raw search query syntax.
func (a *AdvancedSearch) Build() string {
	var parts []string

	if a.AllWords != "" {
		parts = append(parts, a.AllWords)
	}
	if a.ExactPhrase != "" {
		safe := strings.ReplaceAll(a.ExactPhrase, "\"", "")
		parts = append(parts, "\""+safe+"\"")
	}
	if len(a.AnyWords) > 0 {
		parts = append(parts, "("+strings.Join(a.AnyWords, " OR ")+")")
	}
	for _, w := range a.NoneWords {
		parts = append(parts, "-"+w)
	}
	for _, h := range a.Hashtags {
		tag := h
		if !strings.HasPrefix(tag, "#") {
			tag = "#" + tag
		}
		parts = append(parts, tag)
	}
	if a.Language != "" {
		parts = append(parts, "lang:"+a.Language)
	}
	for _, u := range a.From {
		parts = append(parts, "from:"+strings.TrimPrefix(u, "@"))
	}
	for _, u := range a.To {
		parts = append(parts, "to:"+strings.TrimPrefix(u, "@"))
	}
	for _, u := range a.Mentioning {
		parts = append(parts, "@"+strings.TrimPrefix(u, "@"))
	}
	if a.Replies != "" {
		parts = append(parts, string(a.Replies))
	}
	if a.Links != "" {
		parts = append(parts, string(a.Links))
	}
	if a.MinReplies > 0 {
		parts = append(parts, fmt.Sprintf("min_replies:%d", a.MinReplies))
	}
	if a.MinLikes > 0 {
		parts = append(parts, fmt.Sprintf("min_faves:%d", a.MinLikes))
	}
	if a.MinReposts > 0 {
		parts = append(parts, fmt.Sprintf("min_retweets:%d", a.MinReposts))
	}
	if a.Since != "" {
		parts = append(parts, "since:"+a.Since)
	}
	if a.Until != "" {
		parts = append(parts, "until:"+a.Until)
	}

	return strings.Join(parts, " ")
}

// AdvancedSearchTweets executes an advanced search and returns the first page.
func (c *Client) AdvancedSearchTweets(ctx context.Context, search *AdvancedSearch, count int) (TweetPage, error) {
	return c.AdvancedSearchTweetsPage(ctx, search, count, "")
}

// AdvancedSearchTweetsPage executes an advanced search with cursor pagination.
func (c *Client) AdvancedSearchTweetsPage(ctx context.Context, search *AdvancedSearch, count int, cursor string) (TweetPage, error) {
	if search == nil {
		return TweetPage{}, fmt.Errorf("%w: search must not be nil", ErrInvalidParams)
	}
	q := search.Build()
	if q == "" {
		return TweetPage{}, fmt.Errorf("%w: search query is empty — set at least one field", ErrInvalidParams)
	}
	if count <= 0 {
		count = 20
	}

	resultType := search.ResultType
	if resultType == "" {
		resultType = SearchTop
	}

	vars := map[string]interface{}{
		"rawQuery":    q,
		"count":       count,
		"querySource": "typed_query",
		"product":     string(resultType),
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
	return c.SearchUsersPage(ctx, query, count, "")
}

// SearchUsersPage returns a page of user search results starting from cursor.
func (c *Client) SearchUsersPage(ctx context.Context, query string, count int, cursor string) (UserPage, error) {
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
