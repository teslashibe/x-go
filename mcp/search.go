package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// SearchTweetsInput is the typed input for x_search_tweets.
type SearchTweetsInput struct {
	Query      string `json:"query" jsonschema:"description=raw X search query (supports operators like from:user since:YYYY-MM-DD),required"`
	Count      int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
	SearchType string `json:"search_type,omitempty" jsonschema:"description=search tab; allowed: Top,Latest,People,Media,Lists,default=Top"`
	Since      string `json:"since,omitempty" jsonschema:"description=only tweets on or after this date (YYYY-MM-DD)"`
	Until      string `json:"until,omitempty" jsonschema:"description=only tweets on or before this date (YYYY-MM-DD)"`
}

func searchTweets(ctx context.Context, c *x.Client, in SearchTweetsInput) (any, error) {
	opts := []x.SearchOption{}
	if in.SearchType != "" {
		opts = append(opts, x.WithSearchType(x.SearchType(in.SearchType)))
	}
	if in.Since != "" {
		opts = append(opts, x.WithSearchSince(in.Since))
	}
	if in.Until != "" {
		opts = append(opts, x.WithSearchUntil(in.Until))
	}
	res, err := c.SearchTweetsPage(ctx, in.Query, in.Count, in.Cursor, opts...)
	if err != nil {
		return nil, err
	}
	limit := in.Count
	if limit <= 0 {
		limit = 20
	}
	return mcptool.PageOf(res.Tweets, res.NextCursor, limit), nil
}

// SearchUsersInput is the typed input for x_search_users.
type SearchUsersInput struct {
	Query  string `json:"query" jsonschema:"description=keywords or handle to match users on,required"`
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func searchUsers(ctx context.Context, c *x.Client, in SearchUsersInput) (any, error) {
	res, err := c.SearchUsersPage(ctx, in.Query, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	limit := in.Count
	if limit <= 0 {
		limit = 20
	}
	return mcptool.PageOf(res.Users, res.NextCursor, limit), nil
}

// AdvancedSearchTweetsInput is the typed input for x_advanced_search_tweets.
// Mirrors the fields of x.AdvancedSearch and exposes them as a flat schema
// so an agent can build a query without learning X's operator syntax.
type AdvancedSearchTweetsInput struct {
	AllWords    string   `json:"all_words,omitempty" jsonschema:"description=all of these words (space-separated, ANDed)"`
	ExactPhrase string   `json:"exact_phrase,omitempty" jsonschema:"description=this exact phrase (quoted in the resulting query)"`
	AnyWords    []string `json:"any_words,omitempty" jsonschema:"description=any of these words (ORed)"`
	NoneWords   []string `json:"none_words,omitempty" jsonschema:"description=none of these words (excluded)"`
	Hashtags    []string `json:"hashtags,omitempty" jsonschema:"description=hashtags to match (without the leading #)"`
	Language    string   `json:"language,omitempty" jsonschema:"description=BCP-47 language code (e.g. en, es, ja)"`
	From        []string `json:"from,omitempty" jsonschema:"description=tweets from these accounts (without @)"`
	To          []string `json:"to,omitempty" jsonschema:"description=tweets sent in reply to these accounts (without @)"`
	Mentioning  []string `json:"mentioning,omitempty" jsonschema:"description=tweets mentioning these accounts (without @)"`
	Replies     string   `json:"replies,omitempty" jsonschema:"description=reply filter; allowed: ''(any), 'exclude:replies', 'filter:replies'"`
	Links       string   `json:"links,omitempty" jsonschema:"description=link filter; allowed: ''(any), 'exclude:links', 'filter:links'"`
	MinReplies  int      `json:"min_replies,omitempty" jsonschema:"description=minimum reply count,minimum=0"`
	MinLikes    int      `json:"min_likes,omitempty" jsonschema:"description=minimum like count,minimum=0"`
	MinReposts  int      `json:"min_reposts,omitempty" jsonschema:"description=minimum retweet count,minimum=0"`
	Since       string   `json:"since,omitempty" jsonschema:"description=only tweets on or after this date (YYYY-MM-DD)"`
	Until       string   `json:"until,omitempty" jsonschema:"description=only tweets on or before this date (YYYY-MM-DD)"`
	ResultType  string   `json:"result_type,omitempty" jsonschema:"description=search tab; allowed: Top,Latest,People,Media,Lists,default=Top"`

	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func advancedSearchTweets(ctx context.Context, c *x.Client, in AdvancedSearchTweetsInput) (any, error) {
	resultType := x.SearchType(in.ResultType)
	if resultType == "" {
		resultType = x.SearchTop
	}
	search := &x.AdvancedSearch{
		AllWords:    in.AllWords,
		ExactPhrase: in.ExactPhrase,
		AnyWords:    in.AnyWords,
		NoneWords:   in.NoneWords,
		Hashtags:    in.Hashtags,
		Language:    in.Language,
		From:        in.From,
		To:          in.To,
		Mentioning:  in.Mentioning,
		Replies:     x.ReplyFilter(in.Replies),
		Links:       x.LinkFilter(in.Links),
		MinReplies:  in.MinReplies,
		MinLikes:    in.MinLikes,
		MinReposts:  in.MinReposts,
		Since:       in.Since,
		Until:       in.Until,
		ResultType:  resultType,
	}
	res, err := c.AdvancedSearchTweetsPage(ctx, search, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	limit := in.Count
	if limit <= 0 {
		limit = 20
	}
	return mcptool.PageOf(res.Tweets, res.NextCursor, limit), nil
}

var searchTools = []mcptool.Tool{
	mcptool.Define[*x.Client, SearchTweetsInput](
		"x_search_tweets",
		"Search X for tweets matching a query (cursor-paginated; supports search_type, since, until)",
		"SearchTweetsPage",
		searchTweets,
	),
	mcptool.Define[*x.Client, SearchUsersInput](
		"x_search_users",
		"Search X for users matching a query (cursor-paginated)",
		"SearchUsersPage",
		searchUsers,
	),
	mcptool.Define[*x.Client, AdvancedSearchTweetsInput](
		"x_advanced_search_tweets",
		"Search X tweets with the full Advanced Search filter set (cursor-paginated)",
		"AdvancedSearchTweetsPage",
		advancedSearchTweets,
	),
}
