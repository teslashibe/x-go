package x

import (
	"context"
	"encoding/json"
	"fmt"
)

const maxTweetLength = 280

func applyTweetOpts(opts []TweetOption) *tweetOptions {
	o := &tweetOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func buildMediaVars(o *tweetOptions) map[string]interface{} {
	entities := make([]map[string]interface{}, 0, len(o.mediaIDs))
	for _, id := range o.mediaIDs {
		entities = append(entities, map[string]interface{}{
			"media_id": id, "tagged_users": []string{},
		})
	}
	return map[string]interface{}{
		"media_entities":     entities,
		"possibly_sensitive": o.possiblySensitive,
	}
}

// CreateTweet publishes a new tweet.
func (c *Client) CreateTweet(ctx context.Context, text string, opts ...TweetOption) (*Tweet, error) {
	if text == "" {
		return nil, ErrInvalidParams
	}
	if len([]rune(text)) > maxTweetLength {
		return nil, ErrTweetTooLong
	}

	o := applyTweetOpts(opts)
	vars := map[string]interface{}{
		"tweet_text":              text,
		"dark_request":            false,
		"media":                   buildMediaVars(o),
		"semantic_annotation_ids": []interface{}{},
	}

	data, err := c.graphqlPOST(ctx, "CreateTweet", vars)
	if err != nil {
		return nil, err
	}

	return parseTweetFromCreateResponse(data)
}

// Reply publishes a reply to an existing tweet.
func (c *Client) Reply(ctx context.Context, inReplyToID, text string, opts ...TweetOption) (*Tweet, error) {
	if inReplyToID == "" || text == "" {
		return nil, ErrInvalidParams
	}
	if len([]rune(text)) > maxTweetLength {
		return nil, ErrTweetTooLong
	}

	o := applyTweetOpts(opts)
	vars := map[string]interface{}{
		"tweet_text":   text,
		"dark_request": false,
		"reply": map[string]interface{}{
			"in_reply_to_tweet_id":   inReplyToID,
			"exclude_reply_user_ids": []string{},
		},
		"media":                   buildMediaVars(o),
		"semantic_annotation_ids": []interface{}{},
	}

	data, err := c.graphqlPOST(ctx, "CreateTweet", vars)
	if err != nil {
		return nil, err
	}
	return parseTweetFromCreateResponse(data)
}

// QuoteTweet publishes a quote tweet.
func (c *Client) QuoteTweet(ctx context.Context, quotedTweetURL, text string, opts ...TweetOption) (*Tweet, error) {
	if quotedTweetURL == "" || text == "" {
		return nil, ErrInvalidParams
	}
	if len([]rune(text)) > maxTweetLength {
		return nil, ErrTweetTooLong
	}

	o := applyTweetOpts(opts)
	vars := map[string]interface{}{
		"tweet_text":              text,
		"attachment_url":          quotedTweetURL,
		"dark_request":            false,
		"media":                   buildMediaVars(o),
		"semantic_annotation_ids": []interface{}{},
	}

	data, err := c.graphqlPOST(ctx, "CreateTweet", vars)
	if err != nil {
		return nil, err
	}
	return parseTweetFromCreateResponse(data)
}

// DeleteTweet deletes a tweet owned by the authenticated user.
func (c *Client) DeleteTweet(ctx context.Context, tweetID string) error {
	if tweetID == "" {
		return ErrInvalidParams
	}
	vars := map[string]interface{}{
		"tweet_id":     tweetID,
		"dark_request": false,
	}
	_, err := c.graphqlPOST(ctx, "DeleteTweet", vars)
	return err
}

// parseTweetFromCreateResponse extracts a Tweet from the CreateTweet mutation response.
func parseTweetFromCreateResponse(data json.RawMessage) (*Tweet, error) {
	var wrapper struct {
		CreateTweet struct {
			TweetResults struct {
				Result tweetObj `json:"result"`
			} `json:"tweet_results"`
		} `json:"create_tweet"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("%w: parsing create tweet response: %v", ErrRequestFailed, err)
	}
	tweet := toTweet(wrapper.CreateTweet.TweetResults.Result)
	if tweet.ID == "" {
		return nil, fmt.Errorf("%w: tweet creation returned empty ID", ErrRequestFailed)
	}
	return &tweet, nil
}
