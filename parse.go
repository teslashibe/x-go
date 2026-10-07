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
// Envelope rule: when data is present and not null it is parsed and errors
// are ignored, because a page can carry errors for posts that are
// unavailable. Otherwise the first error is mapped to a sentinel as the client
// does. Malformed bodies are ErrRequestFailed.

// ParseSearchTimeline parses one raw SearchTimeline response body. Tweets keep
// Raw envelopes (SchemaVersion 1, Provider "x_graphql") exactly as SearchTweetsPage does.
func ParseSearchTimeline(body []byte) (TweetPage, error) {
	data, err := responseData(body)
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
	data, err := responseData(body)
	if err != nil {
		return nil, err
	}
	return parseTweetDetailData(data, focalTweetID)
}

// ParseUserByScreenName parses one raw UserByScreenName body.
func ParseUserByScreenName(body []byte) (*User, error) {
	data, err := responseData(body)
	if err != nil {
		return nil, err
	}
	return parseUserData(data)
}

// ParseTweetResultByRestID parses one raw TweetResultByRestId body.
func ParseTweetResultByRestID(body []byte) (*Tweet, error) {
	data, err := responseData(body)
	if err != nil {
		return nil, err
	}
	return parseTweetResultData(data)
}

// responseData applies the envelope rule above. Error text never includes
// the body.
func responseData(body []byte) (json.RawMessage, error) {
	// Errors stay raw until needed so an unexpected error shape cannot
	// reject a page whose data is usable.
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: decoding response: %v", ErrRequestFailed, err)
	}
	if len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		return envelope.Data, nil
	}
	var errs []gqlError
	if len(envelope.Errors) > 0 && string(envelope.Errors) != "null" {
		if err := json.Unmarshal(envelope.Errors, &errs); err != nil {
			return nil, fmt.Errorf("%w: decoding errors: %v", ErrRequestFailed, err)
		}
	}
	if len(errs) > 0 {
		return nil, classifyGQLError(errs[0])
	}
	return nil, fmt.Errorf("%w: no data in response", ErrRequestFailed)
}
