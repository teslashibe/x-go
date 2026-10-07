package x

import "context"

// ReplyToPost publishes text as a reply to postID; errors are *OutcomeError
// (ErrDefinite or ErrAmbiguous), as for CreatePost.
func (c *Client) ReplyToPost(ctx context.Context, postID, text string, opts ...TweetOption) (*Tweet, error) {
	return writeOutcome(c.Reply(ctx, postID, text, opts...))
}
