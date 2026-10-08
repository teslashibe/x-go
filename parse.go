package x

import (
	"encoding/json"
	"fmt"
)

// The Parse functions read raw GraphQL response bodies fetched elsewhere, for
// example the pages a Scarlett x_read job returns. Each takes the whole body
// ({"data":…,"errors":…}) and shares its parser with the matching Client
// method, so both paths build identical values.
//
// Envelope rule: when data holds the operation's root field
// (search_by_raw_query, threaded_conversation_with_injections_v2, user or
// tweetResult) with a value other than null, {} or [], it is parsed and errors
// are ignored, because a page can carry errors for posts that are
// unavailable. Otherwise the first error is mapped to a sentinel as the
// client does, so {"data":{},"errors":[{"code":88}]} is ErrRateLimited, not
// ErrNotFound. A body with neither a usable root nor errors goes to the
// operation's parser (a missing root is ErrNotFound). Malformed bodies are
// ErrRequestFailed.

// ParseSearchTimeline parses one raw SearchTimeline response body. Tweets keep
// Raw envelopes (SchemaVersion 1, Provider "x_graphql") exactly as SearchTweetsPage does.
func ParseSearchTimeline(body []byte) (TweetPage, error) {
	data, err := responseData(body, "search_by_raw_query")
	if err != nil {
		return TweetPage{}, err
	}
	return parseSearchData(data)
}

// ParseTweetDetail parses one raw TweetDetail body for focalTweetID (first page only).
func ParseTweetDetail(body []byte, focalTweetID string) (*TweetDetail, error) {
	if focalTweetID == "" {
		return nil, fmt.Errorf("%w: focalTweetID must not be empty", ErrInvalidParams)
	}
	data, err := responseData(body, "threaded_conversation_with_injections_v2")
	if err != nil {
		return nil, err
	}
	return parseTweetDetailData(data, focalTweetID)
}

// ParseUserByScreenName parses one raw UserByScreenName body.
func ParseUserByScreenName(body []byte) (*User, error) {
	data, err := responseData(body, "user")
	if err != nil {
		return nil, err
	}
	return parseUserData(data)
}

// ParseTweetResultByRestID parses one raw TweetResultByRestId body.
func ParseTweetResultByRestID(body []byte) (*Tweet, error) {
	data, err := responseData(body, "tweetResult")
	if err != nil {
		return nil, err
	}
	return parseTweetResultData(data)
}

// responseData applies the envelope rule above for the operation whose data
// root is root. Error text never includes the body.
func responseData(body []byte, root string) (json.RawMessage, error) {
	// Errors stay raw until needed so an unexpected error shape cannot
	// reject a page whose data is usable.
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: decoding response: %v", ErrRequestFailed, err)
	}
	if usableData(envelope.Data, root) {
		return envelope.Data, nil
	}
	var errs []gqlError
	if presentJSON(envelope.Errors) {
		if err := json.Unmarshal(envelope.Errors, &errs); err != nil {
			return nil, fmt.Errorf("%w: decoding errors: %v", ErrRequestFailed, err)
		}
	}
	if len(errs) > 0 {
		return nil, classifyGQLError(errs[0])
	}
	if presentJSON(envelope.Data) {
		return envelope.Data, nil
	}
	return nil, fmt.Errorf("%w: no data in response", ErrRequestFailed)
}
