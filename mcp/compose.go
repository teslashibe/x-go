package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// CreateTweetInput is the typed input for x_create_tweet.
type CreateTweetInput struct {
	Text              string   `json:"text" jsonschema:"description=tweet body (≤280 chars),required"`
	MediaIDs          []string `json:"media_ids,omitempty" jsonschema:"description=optional list of pre-uploaded media IDs to attach"`
	PossiblySensitive bool     `json:"possibly_sensitive,omitempty" jsonschema:"description=mark attached media as possibly sensitive"`
}

func tweetOptions(mediaIDs []string, possiblySensitive bool) []x.TweetOption {
	var opts []x.TweetOption
	if len(mediaIDs) > 0 {
		opts = append(opts, x.WithMediaIDs(mediaIDs...))
	}
	if possiblySensitive {
		opts = append(opts, x.WithPossiblySensitive())
	}
	return opts
}

func createTweet(ctx context.Context, c *x.Client, in CreateTweetInput) (any, error) {
	tw, err := c.CreateTweet(ctx, in.Text, tweetOptions(in.MediaIDs, in.PossiblySensitive)...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "tweet_id": tw.ID, "tweet": tw}, nil
}

// ReplyInput is the typed input for x_reply.
type ReplyInput struct {
	InReplyToID       string   `json:"in_reply_to_id" jsonschema:"description=tweet ID being replied to,required"`
	Text              string   `json:"text" jsonschema:"description=reply body (≤280 chars),required"`
	MediaIDs          []string `json:"media_ids,omitempty" jsonschema:"description=optional list of pre-uploaded media IDs to attach"`
	PossiblySensitive bool     `json:"possibly_sensitive,omitempty" jsonschema:"description=mark attached media as possibly sensitive"`
}

func reply(ctx context.Context, c *x.Client, in ReplyInput) (any, error) {
	tw, err := c.Reply(ctx, in.InReplyToID, in.Text, tweetOptions(in.MediaIDs, in.PossiblySensitive)...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "tweet_id": tw.ID, "in_reply_to_id": in.InReplyToID, "tweet": tw}, nil
}

// QuoteTweetInput is the typed input for x_quote_tweet.
type QuoteTweetInput struct {
	QuotedTweetURL    string   `json:"quoted_tweet_url" jsonschema:"description=full URL of the tweet being quoted (https://x.com/<user>/status/<id>),required"`
	Text              string   `json:"text" jsonschema:"description=quote tweet body (≤280 chars),required"`
	MediaIDs          []string `json:"media_ids,omitempty" jsonschema:"description=optional list of pre-uploaded media IDs to attach"`
	PossiblySensitive bool     `json:"possibly_sensitive,omitempty" jsonschema:"description=mark attached media as possibly sensitive"`
}

func quoteTweet(ctx context.Context, c *x.Client, in QuoteTweetInput) (any, error) {
	tw, err := c.QuoteTweet(ctx, in.QuotedTweetURL, in.Text, tweetOptions(in.MediaIDs, in.PossiblySensitive)...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "tweet_id": tw.ID, "quoted_tweet_url": in.QuotedTweetURL, "tweet": tw}, nil
}

// DeleteTweetInput is the typed input for x_delete_tweet.
type DeleteTweetInput struct {
	TweetID string `json:"tweet_id" jsonschema:"description=ID of the tweet to delete (must be owned by the authenticated user),required"`
}

func deleteTweet(ctx context.Context, c *x.Client, in DeleteTweetInput) (any, error) {
	if err := c.DeleteTweet(ctx, in.TweetID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "tweet_id": in.TweetID}, nil
}

var composeTools = []mcptool.Tool{
	mcptool.Define[*x.Client, CreateTweetInput](
		"x_create_tweet",
		"Publish a new tweet (≤280 chars; optional media attachments)",
		"CreateTweet",
		createTweet,
	),
	mcptool.Define[*x.Client, ReplyInput](
		"x_reply",
		"Publish a reply to an existing tweet",
		"Reply",
		reply,
	),
	mcptool.Define[*x.Client, QuoteTweetInput](
		"x_quote_tweet",
		"Publish a quote tweet referencing another tweet by URL",
		"QuoteTweet",
		quoteTweet,
	),
	mcptool.Define[*x.Client, DeleteTweetInput](
		"x_delete_tweet",
		"Delete a tweet owned by the authenticated user",
		"DeleteTweet",
		deleteTweet,
	),
}
