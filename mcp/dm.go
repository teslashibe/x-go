package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// SendDMInput is the typed input for x_send_dm.
type SendDMInput struct {
	ConversationID string `json:"conversation_id" jsonschema:"description=existing DM conversation ID (e.g. '<id1>-<id2>'),required"`
	Text           string `json:"text" jsonschema:"description=message body,required"`
}

func sendDM(ctx context.Context, c *x.Client, in SendDMInput) (any, error) {
	msg, err := c.SendDM(ctx, in.ConversationID, in.Text)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "conversation_id": in.ConversationID, "message_id": msg.ID, "message": msg}, nil
}

// SendNewDMInput is the typed input for x_send_new_dm.
type SendNewDMInput struct {
	RecipientID string `json:"recipient_id" jsonschema:"description=numeric X user ID of the recipient (creates a 1:1 conversation if none exists),required"`
	Text        string `json:"text" jsonschema:"description=message body,required"`
}

func sendNewDM(ctx context.Context, c *x.Client, in SendNewDMInput) (any, error) {
	msg, err := c.SendNewDM(ctx, in.RecipientID, in.Text)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "recipient_id": in.RecipientID, "conversation_id": msg.ConversationID, "message_id": msg.ID, "message": msg}, nil
}

// GetConversationsInput is the typed input for x_get_conversations.
type GetConversationsInput struct{}

func getConversations(ctx context.Context, c *x.Client, _ GetConversationsInput) (any, error) {
	res, err := c.GetConversations(ctx)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res.Conversations, res.NextCursor, 0), nil
}

// GetConversationInput is the typed input for x_get_conversation.
type GetConversationInput struct {
	ConversationID string `json:"conversation_id" jsonschema:"description=DM conversation ID to fetch messages from,required"`
}

func getConversation(ctx context.Context, c *x.Client, in GetConversationInput) (any, error) {
	res, err := c.GetConversation(ctx, in.ConversationID)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res.Messages, res.NextCursor, 0), nil
}

var dmTools = []mcptool.Tool{
	mcptool.Define[*x.Client, SendDMInput](
		"x_send_dm",
		"Send a direct message in an existing X DM conversation",
		"SendDM",
		sendDM,
	),
	mcptool.Define[*x.Client, SendNewDMInput](
		"x_send_new_dm",
		"Send a direct message to a user by ID, creating a new conversation if needed",
		"SendNewDM",
		sendNewDM,
	),
	mcptool.Define[*x.Client, GetConversationsInput](
		"x_get_conversations",
		"List the authenticated user's DM conversations",
		"GetConversations",
		getConversations,
	),
	mcptool.Define[*x.Client, GetConversationInput](
		"x_get_conversation",
		"Fetch the messages of a specific DM conversation by ID",
		"GetConversation",
		getConversation,
	),
}
