package x

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetTweet retrieves a single tweet by its ID.
func (c *Client) GetTweet(ctx context.Context, tweetID string) (*Tweet, error) {
	if tweetID == "" {
		return nil, fmt.Errorf("%w: tweetID must not be empty", ErrInvalidParams)
	}

	vars := map[string]interface{}{
		"tweetId":                tweetID,
		"withCommunity":          true,
		"includePromotedContent": false,
		"withVoice":              false,
	}

	raw, err := c.graphqlGET(ctx, "TweetResultByRestId", vars)
	if err != nil {
		return nil, err
	}

	return parseTweetResultData(raw)
}

// parseTweetResultData parses the data object of a TweetResultByRestId
// response. Tombstones, unavailable posts and empty results are ErrNotFound.
func parseTweetResultData(data json.RawMessage) (*Tweet, error) {
	var payload struct {
		TweetResult struct {
			Result json.RawMessage `json:"result"`
		} `json:"tweetResult"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("%w: decoding tweet: %v", ErrRequestFailed, err)
	}

	t, ok, err := (tweetResult{Result: payload.TweetResult.Result}).tweet()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return &t, nil
}

// GetTweetDetail retrieves a tweet and its conversation thread (replies).
func (c *Client) GetTweetDetail(ctx context.Context, tweetID string) (*TweetDetail, error) {
	if tweetID == "" {
		return nil, fmt.Errorf("%w: tweetID must not be empty", ErrInvalidParams)
	}

	vars := map[string]interface{}{
		"focalTweetId":                           tweetID,
		"with_rux_injections":                    false,
		"includePromotedContent":                 false,
		"withCommunity":                          true,
		"withQuickPromoteEligibilityTweetFields": true,
		"withBirdwatchNotes":                     true,
		"withVoice":                              true,
		"withV2Timeline":                         true,
	}

	raw, err := c.graphqlGET(ctx, "TweetDetail", vars)
	if err != nil {
		return nil, err
	}

	return parseTweetDetailData(raw, tweetID)
}

// parseTweetDetailData parses the data object of a TweetDetail response for
// tweetID. Posts before the focal post are its ancestors; posts after it are
// replies. A missing conversation or focal tweet is ErrNotFound.
func parseTweetDetailData(data json.RawMessage, tweetID string) (*TweetDetail, error) {
	var payload struct {
		ThreadedConversation json.RawMessage `json:"threaded_conversation_with_injections_v2"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("%w: decoding tweet detail: %v", ErrRequestFailed, err)
	}
	if len(payload.ThreadedConversation) == 0 || string(payload.ThreadedConversation) == "null" {
		return nil, ErrNotFound
	}

	var tl struct {
		Instructions []timelineInstruction `json:"instructions"`
	}
	if err := json.Unmarshal(payload.ThreadedConversation, &tl); err != nil {
		return nil, fmt.Errorf("%w: decoding conversation: %v", ErrRequestFailed, err)
	}

	// Entries arrive in conversation order: the focal post's parent chain
	// (when it is a reply), the focal post, then replies.
	detail := &TweetDetail{}
	focalSeen := false
	add := func(tw Tweet) {
		switch {
		case tw.ID == "":
		case tw.ID == tweetID:
			if !focalSeen {
				detail.Tweet = tw
				focalSeen = true
			}
		case focalSeen:
			detail.Replies = append(detail.Replies, tw)
		default:
			detail.Ancestors = append(detail.Ancestors, tw)
		}
	}
	for _, inst := range tl.Instructions {
		for _, entry := range inst.Entries {
			tweet, ok, err := extractTweetFromEntry(entry)
			if err != nil {
				return nil, err
			}
			if ok && tweet.ID != "" {
				add(tweet)
				continue
			}
			for _, item := range entry.Content.Items {
				if item.Item.ItemContent.TweetResults != nil {
					tw, ok, err := item.Item.ItemContent.TweetResults.tweet()
					if err != nil {
						return nil, err
					}
					if ok {
						add(tw)
					}
				}
			}
		}
	}

	if detail.Tweet.ID == "" {
		return nil, ErrNotFound
	}

	return detail, nil
}

// UserTweets returns the first page of tweets for a given user.
func (c *Client) UserTweets(ctx context.Context, userID string, count int) (TweetPage, error) {
	return c.UserTweetsPage(ctx, userID, count, "")
}

// UserTweetsPage returns a page of tweets for a given user, starting from cursor.
func (c *Client) UserTweetsPage(ctx context.Context, userID string, count int, cursor string) (TweetPage, error) {
	if userID == "" {
		return TweetPage{}, fmt.Errorf("%w: userID must not be empty", ErrInvalidParams)
	}
	if count <= 0 {
		count = 20
	}

	vars := map[string]interface{}{
		"userId":                                 userID,
		"count":                                  count,
		"includePromotedContent":                 false,
		"withQuickPromoteEligibilityTweetFields": true,
		"withVoice":                              true,
		"withV2Timeline":                         true,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}

	raw, err := c.graphqlGET(ctx, "UserTweets", vars)
	if err != nil {
		return TweetPage{}, err
	}

	return parseTweetPage(raw, "user.result.timeline")
}

// DeletePost deletes postID, a post owned by the authenticated account
// (DeleteTweet), with the typed outcome rules in write.go. DeleteTweet is
// unchanged.
func (c *Client) DeletePost(ctx context.Context, postID string) (WriteResult, error) {
	return c.graphqlWrite(ctx, deletePostOp, postID)
}

// GetPostState reads postID once (TweetResultByRestId) and reports whether X
// returned the post, a tombstone, an unavailable result or nothing. Unlike
// GetTweet it does not fold tombstones into ErrNotFound, and a present post
// carries Tweet.Viewer when X returned every viewer flag. It sends exactly
// one request, never retries and refuses redirects. A 404 without an X error
// body is ErrQueryIDStale; X error codes 34 and 144 stay ErrNotFound.
func (c *Client) GetPostState(ctx context.Context, postID string) (PostState, error) {
	if !xNumericID(postID) {
		return PostState{}, fmt.Errorf("%w: postID must be a numeric X ID", ErrInvalidParams)
	}
	vars := map[string]interface{}{
		"tweetId":                postID,
		"withCommunity":          true,
		"includePromotedContent": false,
		"withVoice":              false,
	}
	data, err := c.graphqlReadOnce(ctx, "TweetResultByRestId", vars)
	if err != nil {
		return PostState{}, err
	}
	return parsePostState(data, postID)
}

// parsePostState reads a TweetResultByRestId data object for postID. A
// response without the tweetResult root, an unknown result type or a post
// with another ID is ErrRequestFailed: the state cannot be verified.
func parsePostState(data json.RawMessage, postID string) (PostState, error) {
	var payload struct {
		TweetResult *struct {
			Result json.RawMessage `json:"result"`
		} `json:"tweetResult"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return PostState{}, fmt.Errorf("%w: decoding post state: %v", ErrRequestFailed, err)
	}
	if payload.TweetResult == nil {
		return PostState{}, errStateUnverifiable("post state: no tweetResult root")
	}
	result := payload.TweetResult.Result
	if !nonEmptyJSON(result) {
		return PostState{Kind: PostEmpty}, nil
	}
	var head struct {
		Typename  string `json:"__typename"`
		Reason    string `json:"reason"`
		Tombstone *struct {
			Text struct {
				Text string `json:"text"`
			} `json:"text"`
		} `json:"tombstone"`
	}
	if err := json.Unmarshal(result, &head); err != nil {
		return PostState{}, fmt.Errorf("%w: decoding post state: %v", ErrRequestFailed, err)
	}
	switch head.Typename {
	case "TweetTombstone":
		state := PostState{Kind: PostTombstone}
		if head.Tombstone != nil {
			state.Reason = head.Tombstone.Text.Text
		}
		return state, nil
	case "TweetUnavailable":
		return PostState{Kind: PostUnavailable, Reason: head.Reason}, nil
	case "Tweet", "TweetWithVisibilityResults":
		tweet, ok, err := (tweetResult{Result: result}).tweet()
		if err != nil {
			return PostState{}, err
		}
		if !ok || tweet.ID != postID {
			return PostState{}, errStateUnverifiable("post state: result is not the requested post")
		}
		viewer, err := postViewerState(result)
		if err != nil {
			return PostState{}, err
		}
		tweet.Viewer = viewer
		return PostState{Kind: PostPresent, Tweet: &tweet}, nil
	default:
		return PostState{}, errStateUnverifiable("post state: unknown result type")
	}
}

// postViewerState reads legacy.favorited, retweeted and bookmarked from a
// tweet_results.result object. It returns nil unless all three are present.
func postViewerState(result json.RawMessage) (*PostViewerState, error) {
	var o tweetObj
	if err := json.Unmarshal(result, &o); err != nil {
		return nil, fmt.Errorf("%w: decoding viewer state: %v", ErrRequestFailed, err)
	}
	if o.Typename == "TweetWithVisibilityResults" {
		if o.Tweet == nil {
			return nil, nil
		}
		o = *o.Tweet
	}
	l := o.Legacy
	if l.Favorited == nil || l.Retweeted == nil || l.Bookmarked == nil {
		return nil, nil
	}
	return &PostViewerState{Liked: *l.Favorited, Reposted: *l.Retweeted, Bookmarked: *l.Bookmarked}, nil
}
