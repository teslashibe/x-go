package x

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetProfile retrieves a user's profile by screen name (handle).
func (c *Client) GetProfile(ctx context.Context, screenName string) (*User, error) {
	if screenName == "" {
		return nil, fmt.Errorf("%w: screenName must not be empty", ErrInvalidParams)
	}

	vars := map[string]interface{}{
		"screen_name":              screenName,
		"withSafetyModeUserFields": true,
	}

	raw, err := c.graphqlGET(ctx, "UserByScreenName", vars)
	if err != nil {
		return nil, err
	}

	return parseUserData(raw)
}

// GetProfileByID retrieves a user's profile by their numeric REST ID.
func (c *Client) GetProfileByID(ctx context.Context, userID string) (*User, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: userID must not be empty", ErrInvalidParams)
	}

	vars := map[string]interface{}{
		"userId":                   userID,
		"withSafetyModeUserFields": true,
	}

	raw, err := c.graphqlGET(ctx, "UserByRestId", vars)
	if err != nil {
		return nil, err
	}

	return parseUserData(raw)
}

// parseUserData parses the data object of a UserByScreenName or UserByRestId
// response. UserUnavailable is ErrSuspended; a missing user is ErrNotFound.
func parseUserData(data json.RawMessage) (*User, error) {
	var payload struct {
		User struct {
			Result userObj `json:"result"`
		} `json:"user"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("%w: decoding profile: %v", ErrRequestFailed, err)
	}

	if payload.User.Result.Typename == "UserUnavailable" {
		return nil, ErrSuspended
	}
	if payload.User.Result.RestID == "" {
		return nil, ErrNotFound
	}

	u := toUser(payload.User.Result)
	return &u, nil
}
