package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// GetTweetInput is the typed input for x_get_tweet.
type GetTweetInput struct {
	TweetID string `json:"tweet_id" jsonschema:"description=numeric tweet ID,required"`
	View    string `json:"view,omitempty" jsonschema:"description=response view; allowed: full,compact,metrics,default=full"`
}

func getTweet(ctx context.Context, c *x.Client, in GetTweetInput) (any, error) {
	tw, err := c.GetTweet(ctx, in.TweetID)
	if err != nil {
		return nil, err
	}
	return projectTweet(tw, in.View)
}

type tweetEngagementCounts struct {
	LikeCount     int `json:"likeCount"`
	RetweetCount  int `json:"retweetCount"`
	ReplyCount    int `json:"replyCount"`
	QuoteCount    int `json:"quoteCount"`
	BookmarkCount int `json:"bookmarkCount"`
	ViewCount     int `json:"viewCount"`
}

type tweetMetricsView struct {
	ID               string    `json:"id"`
	AuthorScreenName string    `json:"authorScreenName"`
	CreatedAt        time.Time `json:"createdAt"`
	tweetEngagementCounts
}

type tweetCompactView struct {
	ID               string    `json:"id"`
	ConversationID   string    `json:"conversationId,omitempty"`
	AuthorID         string    `json:"authorId"`
	AuthorScreenName string    `json:"authorScreenName"`
	AuthorName       string    `json:"authorName"`
	Text             string    `json:"text"`
	CreatedAt        time.Time `json:"createdAt"`
	IsRetweet        bool      `json:"isRetweet"`
	IsQuote          bool      `json:"isQuote"`
	IsReply          bool      `json:"isReply"`
	InReplyToID      string    `json:"inReplyToId,omitempty"`
	QuotedTweetID    string    `json:"quotedTweetId,omitempty"`
	tweetEngagementCounts
}

func projectTweet(tw *x.Tweet, view string) (any, error) {
	switch view {
	case "", "full":
		return tw, nil
	case "metrics":
		return tweetMetricsView{
			ID:                    tw.ID,
			AuthorScreenName:      tw.AuthorScreenName,
			CreatedAt:             tw.CreatedAt,
			tweetEngagementCounts: engagementCounts(tw),
		}, nil
	case "compact":
		return tweetCompactView{
			ID:                    tw.ID,
			ConversationID:        tw.ConversationID,
			AuthorID:              tw.AuthorID,
			AuthorScreenName:      tw.AuthorScreenName,
			AuthorName:            tw.AuthorName,
			Text:                  tw.Text,
			CreatedAt:             tw.CreatedAt,
			IsRetweet:             tw.IsRetweet,
			IsQuote:               tw.IsQuote,
			IsReply:               tw.IsReply,
			InReplyToID:           tw.InReplyToID,
			QuotedTweetID:         tw.QuotedTweetID,
			tweetEngagementCounts: engagementCounts(tw),
		}, nil
	default:
		return nil, fmt.Errorf("%w: view must be one of full, compact, metrics", x.ErrInvalidParams)
	}
}

func engagementCounts(tw *x.Tweet) tweetEngagementCounts {
	return tweetEngagementCounts{
		LikeCount:     tw.LikeCount,
		RetweetCount:  tw.RetweetCount,
		ReplyCount:    tw.ReplyCount,
		QuoteCount:    tw.QuoteCount,
		BookmarkCount: tw.BookmarkCount,
		ViewCount:     tw.ViewCount,
	}
}

// GetTweetDetailInput is the typed input for x_get_tweet_detail.
type GetTweetDetailInput struct {
	TweetID    string `json:"tweet_id" jsonschema:"description=numeric tweet ID; result includes the tweet plus its reply thread,required"`
	View       string `json:"view,omitempty" jsonschema:"description=response view; allowed: full,compact,metrics,default=full"`
	MaxReplies *int   `json:"max_replies,omitempty" jsonschema:"description=maximum replies to return from the parsed thread,minimum=0,maximum=100,default=20"`
}

const (
	defaultTweetDetailMaxReplies  = 20
	absoluteTweetDetailMaxReplies = 100
)

type tweetDetailView struct {
	Tweet              any   `json:"tweet"`
	Replies            []any `json:"replies"`
	RepliesTruncated   bool  `json:"replies_truncated"`
	ReplyCountReturned int   `json:"reply_count_returned"`
	ReplyCountSeen     int   `json:"reply_count_seen"`
}

func getTweetDetail(ctx context.Context, c *x.Client, in GetTweetDetailInput) (any, error) {
	detail, err := c.GetTweetDetail(ctx, in.TweetID)
	if err != nil {
		return nil, err
	}
	return projectTweetDetail(detail, in.View, in.MaxReplies)
}

func projectTweetDetail(detail *x.TweetDetail, view string, maxReplies *int) (tweetDetailView, error) {
	limit := defaultTweetDetailMaxReplies
	if maxReplies != nil {
		limit = *maxReplies
	}
	if limit < 0 {
		return tweetDetailView{}, fmt.Errorf("%w: max_replies must be at least 0", x.ErrInvalidParams)
	}
	if limit > absoluteTweetDetailMaxReplies {
		return tweetDetailView{}, fmt.Errorf("%w: max_replies must be at most %d", x.ErrInvalidParams, absoluteTweetDetailMaxReplies)
	}

	replyCountSeen := len(detail.Replies)
	if limit > replyCountSeen {
		limit = replyCountSeen
	}

	tweet, err := projectTweetForDetail(&detail.Tweet, view)
	if err != nil {
		return tweetDetailView{}, err
	}

	replies := make([]any, 0, limit)
	for i := 0; i < limit; i++ {
		reply, err := projectTweetForDetail(&detail.Replies[i], view)
		if err != nil {
			return tweetDetailView{}, err
		}
		replies = append(replies, reply)
	}

	return tweetDetailView{
		Tweet:              tweet,
		Replies:            replies,
		RepliesTruncated:   replyCountSeen > len(replies),
		ReplyCountReturned: len(replies),
		ReplyCountSeen:     replyCountSeen,
	}, nil
}

func projectTweetForDetail(tw *x.Tweet, view string) (any, error) {
	if view == "metrics" {
		return engagementCounts(tw), nil
	}
	return projectTweet(tw, view)
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
