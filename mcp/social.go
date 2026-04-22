package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// UserActionInput is the shared typed input for the follow/mute/block family
// of tools, all of which take just a user ID.
type UserActionInput struct {
	UserID string `json:"user_id" jsonschema:"description=numeric X user ID,required"`
}

func follow(ctx context.Context, c *x.Client, in UserActionInput) (any, error) {
	if err := c.Follow(ctx, in.UserID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID}, nil
}

func unfollow(ctx context.Context, c *x.Client, in UserActionInput) (any, error) {
	if err := c.Unfollow(ctx, in.UserID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID}, nil
}

func mute(ctx context.Context, c *x.Client, in UserActionInput) (any, error) {
	if err := c.Mute(ctx, in.UserID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID}, nil
}

func unmute(ctx context.Context, c *x.Client, in UserActionInput) (any, error) {
	if err := c.Unmute(ctx, in.UserID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID}, nil
}

func block(ctx context.Context, c *x.Client, in UserActionInput) (any, error) {
	if err := c.Block(ctx, in.UserID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID}, nil
}

func unblock(ctx context.Context, c *x.Client, in UserActionInput) (any, error) {
	if err := c.Unblock(ctx, in.UserID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "user_id": in.UserID}, nil
}

var socialTools = []mcptool.Tool{
	mcptool.Define[*x.Client, UserActionInput](
		"x_follow",
		"Follow an X user",
		"Follow",
		follow,
	),
	mcptool.Define[*x.Client, UserActionInput](
		"x_unfollow",
		"Unfollow an X user",
		"Unfollow",
		unfollow,
	),
	mcptool.Define[*x.Client, UserActionInput](
		"x_mute",
		"Mute an X user",
		"Mute",
		mute,
	),
	mcptool.Define[*x.Client, UserActionInput](
		"x_unmute",
		"Unmute an X user",
		"Unmute",
		unmute,
	),
	mcptool.Define[*x.Client, UserActionInput](
		"x_block",
		"Block an X user",
		"Block",
		block,
	),
	mcptool.Define[*x.Client, UserActionInput](
		"x_unblock",
		"Unblock an X user",
		"Unblock",
		unblock,
	),
}
