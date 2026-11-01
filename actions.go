package x

import "context"

// Like adds a like to a tweet.
func (c *Client) Like(ctx context.Context, tweetID string) error {
	if tweetID == "" {
		return ErrInvalidParams
	}
	vars := map[string]interface{}{
		"tweet_id": tweetID,
	}
	_, err := c.graphqlPOST(ctx, "FavoriteTweet", vars)
	return err
}

// Unlike removes a like from a tweet.
func (c *Client) Unlike(ctx context.Context, tweetID string) error {
	if tweetID == "" {
		return ErrInvalidParams
	}
	vars := map[string]interface{}{
		"tweet_id": tweetID,
	}
	_, err := c.graphqlPOST(ctx, "UnfavoriteTweet", vars)
	return err
}

// Retweet retweets a tweet.
func (c *Client) Retweet(ctx context.Context, tweetID string) error {
	if tweetID == "" {
		return ErrInvalidParams
	}
	vars := map[string]interface{}{
		"tweet_id":     tweetID,
		"dark_request": false,
	}
	_, err := c.graphqlPOST(ctx, "CreateRetweet", vars)
	return err
}

// Unretweet undoes a retweet.
func (c *Client) Unretweet(ctx context.Context, tweetID string) error {
	if tweetID == "" {
		return ErrInvalidParams
	}
	vars := map[string]interface{}{
		"source_tweet_id": tweetID,
		"dark_request":    false,
	}
	_, err := c.graphqlPOST(ctx, "DeleteRetweet", vars)
	return err
}

// Bookmark bookmarks a tweet.
func (c *Client) Bookmark(ctx context.Context, tweetID string) error {
	if tweetID == "" {
		return ErrInvalidParams
	}
	vars := map[string]interface{}{
		"tweet_id": tweetID,
	}
	_, err := c.graphqlPOST(ctx, "CreateBookmark", vars)
	return err
}

// Unbookmark removes a tweet from bookmarks.
func (c *Client) Unbookmark(ctx context.Context, tweetID string) error {
	if tweetID == "" {
		return ErrInvalidParams
	}
	vars := map[string]interface{}{
		"tweet_id": tweetID,
	}
	_, err := c.graphqlPOST(ctx, "DeleteBookmark", vars)
	return err
}

// The typed writes below send exactly one request and return a WriteResult
// only when X confirmed the change. Every failure is an *OutcomeError that
// says whether the write was not attempted, definitely rejected, or ambiguous
// (it may have been applied). They are never retried. See write.go for the
// outcome rules. Like, Unlike and the other untyped methods are unchanged.

// LikePost likes postID (FavoriteTweet).
func (c *Client) LikePost(ctx context.Context, postID string) (WriteResult, error) {
	return c.graphqlWrite(ctx, likePostOp, postID)
}

// UnlikePost removes a like from postID (UnfavoriteTweet).
func (c *Client) UnlikePost(ctx context.Context, postID string) (WriteResult, error) {
	return c.graphqlWrite(ctx, unlikePostOp, postID)
}

// Repost reposts postID (CreateRetweet).
func (c *Client) Repost(ctx context.Context, postID string) (WriteResult, error) {
	return c.graphqlWrite(ctx, repostOp, postID)
}

// Unrepost undoes a repost of postID (DeleteRetweet).
func (c *Client) Unrepost(ctx context.Context, postID string) (WriteResult, error) {
	return c.graphqlWrite(ctx, unrepostOp, postID)
}

// BookmarkPost bookmarks postID (CreateBookmark).
func (c *Client) BookmarkPost(ctx context.Context, postID string) (WriteResult, error) {
	return c.graphqlWrite(ctx, bookmarkPostOp, postID)
}

// UnbookmarkPost removes postID from bookmarks (DeleteBookmark).
func (c *Client) UnbookmarkPost(ctx context.Context, postID string) (WriteResult, error) {
	return c.graphqlWrite(ctx, unbookmarkPostOp, postID)
}
