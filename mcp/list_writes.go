package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// The list mutation tools return the method's x.WriteResult only when X
// confirmed the change. Every failure is the method's *x.OutcomeError: no
// query id for these operations ships yet, so today each refuses with
// x.ErrQueryIDUnavailable and sends nothing.

// writeOutcome returns the confirmed result, or only the error.
func writeOutcome(res x.WriteResult, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return res, nil
}

// CreateListInput is the typed input for x_create_list.
type CreateListInput struct {
	Name        string `json:"name" jsonschema:"description=list name,required"`
	Description string `json:"description,omitempty" jsonschema:"description=list description"`
	Private     bool   `json:"private,omitempty" jsonschema:"description=create the list as private"`
}

func createList(ctx context.Context, c *x.Client, in CreateListInput) (any, error) {
	return writeOutcome(c.CreateList(ctx, x.ListInput{Name: in.Name, Description: in.Description, Private: in.Private}))
}

// UpdateListInput is the typed input for x_update_list. The name,
// description and visibility replace the list's current values.
type UpdateListInput struct {
	ListID      string `json:"list_id" jsonschema:"description=numeric X list ID,required"`
	Name        string `json:"name" jsonschema:"description=new list name,required"`
	Description string `json:"description,omitempty" jsonschema:"description=new list description (empty clears it)"`
	Private     bool   `json:"private,omitempty" jsonschema:"description=make the list private (false makes it public)"`
}

func updateList(ctx context.Context, c *x.Client, in UpdateListInput) (any, error) {
	return writeOutcome(c.UpdateList(ctx, in.ListID, x.ListInput{Name: in.Name, Description: in.Description, Private: in.Private}))
}

// ListIDInput is the shared typed input for the tools that take only a list
// ID.
type ListIDInput struct {
	ListID string `json:"list_id" jsonschema:"description=numeric X list ID,required"`
}

func deleteList(ctx context.Context, c *x.Client, in ListIDInput) (any, error) {
	return writeOutcome(c.DeleteList(ctx, in.ListID))
}

func subscribeList(ctx context.Context, c *x.Client, in ListIDInput) (any, error) {
	return writeOutcome(c.SubscribeList(ctx, in.ListID))
}

func unsubscribeList(ctx context.Context, c *x.Client, in ListIDInput) (any, error) {
	return writeOutcome(c.UnsubscribeList(ctx, in.ListID))
}

// ListMemberInput is the typed input for x_add_list_member and
// x_remove_list_member.
type ListMemberInput struct {
	ListID string `json:"list_id" jsonschema:"description=numeric X list ID,required"`
	UserID string `json:"user_id" jsonschema:"description=numeric X user ID of the member,required"`
}

func addListMember(ctx context.Context, c *x.Client, in ListMemberInput) (any, error) {
	return writeOutcome(c.AddListMember(ctx, in.ListID, in.UserID))
}

func removeListMember(ctx context.Context, c *x.Client, in ListMemberInput) (any, error) {
	return writeOutcome(c.RemoveListMember(ctx, in.ListID, in.UserID))
}

// listWriteTools are the list mutations. They come last so the order of the
// older tools is unchanged.
var listWriteTools = []mcptool.Tool{
	mcptool.Define[*x.Client, CreateListInput](
		"x_create_list",
		"Create an X list owned by the authenticated account (typed outcome; the result's targetId is the new list ID)",
		"CreateList",
		createList,
	),
	mcptool.Define[*x.Client, UpdateListInput](
		"x_update_list",
		"Replace the name, description and visibility of an X list (typed outcome)",
		"UpdateList",
		updateList,
	),
	mcptool.Define[*x.Client, ListIDInput](
		"x_delete_list",
		"Delete an X list owned by the authenticated account (typed outcome)",
		"DeleteList",
		deleteList,
	),
	mcptool.Define[*x.Client, ListMemberInput](
		"x_add_list_member",
		"Add a user to an X list (typed outcome)",
		"AddListMember",
		addListMember,
	),
	mcptool.Define[*x.Client, ListMemberInput](
		"x_remove_list_member",
		"Remove a user from an X list (typed outcome)",
		"RemoveListMember",
		removeListMember,
	),
	mcptool.Define[*x.Client, ListIDInput](
		"x_subscribe_list",
		"Subscribe the authenticated account to an X list (typed outcome)",
		"SubscribeList",
		subscribeList,
	),
	mcptool.Define[*x.Client, ListIDInput](
		"x_unsubscribe_list",
		"Unsubscribe the authenticated account from an X list (typed outcome)",
		"UnsubscribeList",
		unsubscribeList,
	),
}
