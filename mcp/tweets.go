package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// GetTweetInput is the typed input for x_get_tweet.
type GetTweetInput struct {
	TweetID string `json:"tweet_id" jsonschema:"description=numeric tweet ID,required"`
}

func getTweet(ctx context.Context, c *x.Client, in GetTweetInput) (any, error) {
	return c.GetTweet(ctx, in.TweetID)
}

// GetTweetDetailInput is the typed input for x_get_tweet_detail.
type GetTweetDetailInput struct {
	TweetID string `json:"tweet_id" jsonschema:"description=numeric tweet ID; result includes the tweet plus its reply thread,required"`
}

func getTweetDetail(ctx context.Context, c *x.Client, in GetTweetDetailInput) (any, error) {
	return c.GetTweetDetail(ctx, in.TweetID)
}

// GetUserTweetsInput is the typed input for x_get_user_tweets.
type GetUserTweetsInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric X user ID whose tweets to fetch,required"`
	Count  int    `json:"count,omitempty" jsonschema:"description=results per page,minimum=1,maximum=200,default=20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque pagination cursor returned by a previous call (next_cursor)"`
}

func getUserTweets(ctx context.Context, c *x.Client, in GetUserTweetsInput) (any, error) {
	res, err := c.UserTweetsPage(ctx, in.UserID, in.Count, in.Cursor)
	if err != nil {
		return nil, err
	}
	limit := in.Count
	if limit <= 0 {
		limit = 20
	}
	return mcptool.PageOf(res.Tweets, res.NextCursor, limit), nil
}

var tweetTools = []mcptool.Tool{
	mcptool.Define[*x.Client, GetTweetInput](
		"x_get_tweet",
		"Fetch a single X tweet by ID",
		"GetTweet",
		getTweet,
	),
	mcptool.Define[*x.Client, GetTweetDetailInput](
		"x_get_tweet_detail",
		"Fetch an X tweet with its conversation thread (focal tweet plus replies)",
		"GetTweetDetail",
		getTweetDetail,
	),
	mcptool.Define[*x.Client, GetUserTweetsInput](
		"x_get_user_tweets",
		"Fetch a page of tweets authored by an X user (cursor-paginated)",
		"UserTweetsPage",
		getUserTweets,
	),
}
