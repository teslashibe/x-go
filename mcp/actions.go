package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// TweetActionInput is the shared typed input for the like/retweet/bookmark
// family of tools, all of which take just a tweet ID.
type TweetActionInput struct {
	TweetID string `json:"tweet_id" jsonschema:"description=numeric tweet ID,required"`
}

func like(ctx context.Context, c *x.Client, in TweetActionInput) (any, error) {
	if err := c.Like(ctx, in.TweetID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "tweet_id": in.TweetID}, nil
}

func unlike(ctx context.Context, c *x.Client, in TweetActionInput) (any, error) {
	if err := c.Unlike(ctx, in.TweetID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "tweet_id": in.TweetID}, nil
}

func retweet(ctx context.Context, c *x.Client, in TweetActionInput) (any, error) {
	if err := c.Retweet(ctx, in.TweetID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "tweet_id": in.TweetID}, nil
}

func unretweet(ctx context.Context, c *x.Client, in TweetActionInput) (any, error) {
	if err := c.Unretweet(ctx, in.TweetID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "tweet_id": in.TweetID}, nil
}

func bookmark(ctx context.Context, c *x.Client, in TweetActionInput) (any, error) {
	if err := c.Bookmark(ctx, in.TweetID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "tweet_id": in.TweetID}, nil
}

func unbookmark(ctx context.Context, c *x.Client, in TweetActionInput) (any, error) {
	if err := c.Unbookmark(ctx, in.TweetID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "tweet_id": in.TweetID}, nil
}

var actionTools = []mcptool.Tool{
	mcptool.Define[*x.Client, TweetActionInput](
		"x_like",
		"Like a tweet",
		"Like",
		like,
	),
	mcptool.Define[*x.Client, TweetActionInput](
		"x_unlike",
		"Remove a like from a tweet",
		"Unlike",
		unlike,
	),
	mcptool.Define[*x.Client, TweetActionInput](
		"x_retweet",
		"Retweet a tweet",
		"Retweet",
		retweet,
	),
	mcptool.Define[*x.Client, TweetActionInput](
		"x_unretweet",
		"Undo a retweet",
		"Unretweet",
		unretweet,
	),
	mcptool.Define[*x.Client, TweetActionInput](
		"x_bookmark",
		"Bookmark a tweet",
		"Bookmark",
		bookmark,
	),
	mcptool.Define[*x.Client, TweetActionInput](
		"x_unbookmark",
		"Remove a tweet from bookmarks",
		"Unbookmark",
		unbookmark,
	),
}
