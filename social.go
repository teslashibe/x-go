package x

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Follow follows a user.
func (c *Client) Follow(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalidParams
	}
	form := url.Values{}
	form.Set("user_id", userID)
	_, err := c.restFormPOST(ctx, "/i/api/1.1/friendships/create.json", form)
	return err
}

// Unfollow unfollows a user.
func (c *Client) Unfollow(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalidParams
	}
	form := url.Values{}
	form.Set("user_id", userID)
	_, err := c.restFormPOST(ctx, "/i/api/1.1/friendships/destroy.json", form)
	return err
}

// Mute mutes a user.
func (c *Client) Mute(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalidParams
	}
	form := url.Values{}
	form.Set("user_id", userID)
	_, err := c.restFormPOST(ctx, "/i/api/1.1/mutes/users/create.json", form)
	return err
}

// Unmute unmutes a user.
func (c *Client) Unmute(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalidParams
	}
	form := url.Values{}
	form.Set("user_id", userID)
	_, err := c.restFormPOST(ctx, "/i/api/1.1/mutes/users/destroy.json", form)
	return err
}

// Block blocks a user.
func (c *Client) Block(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalidParams
	}
	form := url.Values{}
	form.Set("user_id", userID)
	_, err := c.restFormPOST(ctx, "/i/api/1.1/blocks/create.json", form)
	return err
}

// Unblock unblocks a user.
func (c *Client) Unblock(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalidParams
	}
	form := url.Values{}
	form.Set("user_id", userID)
	_, err := c.restFormPOST(ctx, "/i/api/1.1/blocks/destroy.json", form)
	return err
}

// The typed relationship writes below follow the rules in write.go: one
// request, no redirects, no retries, and a WriteResult only when X answered
// 200 with a JSON user whose id_str is userID. Follow, Mute and the other
// untyped methods are unchanged.

// FollowUser follows userID. A protected account gives WriteRequested.
func (c *Client) FollowUser(ctx context.Context, userID string) (WriteResult, error) {
	return c.formWrite(ctx, followUserOp, userID)
}

// UnfollowUser unfollows userID.
func (c *Client) UnfollowUser(ctx context.Context, userID string) (WriteResult, error) {
	return c.formWrite(ctx, unfollowUserOp, userID)
}

// MuteUser mutes userID.
func (c *Client) MuteUser(ctx context.Context, userID string) (WriteResult, error) {
	return c.formWrite(ctx, muteUserOp, userID)
}

// UnmuteUser unmutes userID.
func (c *Client) UnmuteUser(ctx context.Context, userID string) (WriteResult, error) {
	return c.formWrite(ctx, unmuteUserOp, userID)
}

// BlockUser blocks userID.
func (c *Client) BlockUser(ctx context.Context, userID string) (WriteResult, error) {
	return c.formWrite(ctx, blockUserOp, userID)
}

// UnblockUser unblocks userID.
func (c *Client) UnblockUser(ctx context.Context, userID string) (WriteResult, error) {
	return c.formWrite(ctx, unblockUserOp, userID)
}

// GetUserRelationship reads userID once (UserByRestId) and reports the
// authenticated account's relationship with that user from the user's
// relationship_perspectives and follow_request_sent fields. It sends exactly
// one request, never retries and refuses redirects. A response missing any
// of those fields is ErrRequestFailed rather than a guessed false.
func (c *Client) GetUserRelationship(ctx context.Context, userID string) (Relationship, error) {
	if !xNumericID(userID) {
		return Relationship{}, fmt.Errorf("%w: userID must be a numeric X ID", ErrInvalidParams)
	}
	vars := map[string]interface{}{
		"userId":                   userID,
		"withSafetyModeUserFields": true,
	}
	data, err := c.graphqlReadOnce(ctx, "UserByRestId", vars)
	if err != nil {
		return Relationship{}, err
	}
	return parseUserRelationship(data, userID)
}

// parseUserRelationship reads a UserByRestId data object for userID.
// UserUnavailable is ErrSuspended and an empty user root is ErrNotFound, as
// for GetProfileByID; a missing root or field is ErrRequestFailed.
func parseUserRelationship(data json.RawMessage, userID string) (Relationship, error) {
	var payload struct {
		User *struct {
			Result json.RawMessage `json:"result"`
		} `json:"user"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Relationship{}, fmt.Errorf("%w: decoding relationship: %v", ErrRequestFailed, err)
	}
	if payload.User == nil {
		return Relationship{}, errStateUnverifiable("relationship: no user root")
	}
	if !nonEmptyJSON(payload.User.Result) {
		return Relationship{}, ErrNotFound
	}
	var user struct {
		Typename    string `json:"__typename"`
		RestID      string `json:"rest_id"`
		Perspective *struct {
			Following  *bool `json:"following"`
			FollowedBy *bool `json:"followed_by"`
			Muting     *bool `json:"muting"`
			Blocking   *bool `json:"blocking"`
			BlockedBy  *bool `json:"blocked_by"`
		} `json:"relationship_perspectives"`
		FollowRequestSent *bool `json:"follow_request_sent"`
		Legacy            struct {
			FollowRequestSent *bool `json:"follow_request_sent"`
		} `json:"legacy"`
	}
	if err := json.Unmarshal(payload.User.Result, &user); err != nil {
		return Relationship{}, fmt.Errorf("%w: decoding relationship: %v", ErrRequestFailed, err)
	}
	if user.Typename == "UserUnavailable" {
		return Relationship{}, ErrSuspended
	}
	if user.RestID != userID {
		return Relationship{}, errStateUnverifiable("relationship: result is not the requested user")
	}
	requested := user.FollowRequestSent
	if requested == nil {
		requested = user.Legacy.FollowRequestSent
	}
	p := user.Perspective
	if p == nil || p.Following == nil || p.FollowedBy == nil || p.Muting == nil || p.Blocking == nil || p.BlockedBy == nil || requested == nil {
		return Relationship{}, errStateUnverifiable("relationship: response lacks relationship fields")
	}
	return Relationship{
		Following:         *p.Following,
		FollowedBy:        *p.FollowedBy,
		FollowRequestSent: *requested,
		Muting:            *p.Muting,
		Blocking:          *p.Blocking,
		BlockedBy:         *p.BlockedBy,
	}, nil
}
