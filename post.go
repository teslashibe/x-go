package x

import "context"

// CreatePost publishes an original post; errors are *OutcomeError (ErrDefinite or ErrAmbiguous).
func (c *Client) CreatePost(ctx context.Context, text string, opts ...TweetOption) (*Tweet, error) {
	return writeOutcome(c.CreateTweet(ctx, text, opts...))
}

// QuotePost publishes a quote of quotedTweetURL; same classification.
func (c *Client) QuotePost(ctx context.Context, quotedTweetURL, text string, opts ...TweetOption) (*Tweet, error) {
	return writeOutcome(c.QuoteTweet(ctx, quotedTweetURL, text, opts...))
}

// writeOutcome classifies a compose result. A success without a post ID is
// ambiguous: X may have published the post without acknowledging it.
func writeOutcome(tweet *Tweet, err error) (*Tweet, error) {
	if err != nil {
		return nil, classifyWriteOutcome(err)
	}
	if tweet == nil || tweet.ID == "" {
		return nil, &OutcomeError{Outcome: ErrAmbiguous, Err: errMissingAck}
	}
	return tweet, nil
}
