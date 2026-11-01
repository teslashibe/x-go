package x

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ListInput is the full name, description and visibility of an X list, as
// CreateList sends it and UpdateList replaces it.
type ListInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
}

// List mutations (CreateList, UpdateList, DeleteList, AddListMember,
// RemoveListMember, SubscribeList, UnsubscribeList) are typed GraphQL writes
// with the same outcome rules as LikePost (see write.go): exactly one
// request, never retried, redirects refused as ambiguous, and every failure
// an *OutcomeError.
//
// No query id for any of them ships in defaultQueryIDs: query ids enter the
// defaults only from a Live capture. Until then each method returns an
// *OutcomeError that is NotAttempted and matches ErrQueryIDUnavailable, and
// sends nothing.
//
// The operation names, variables and ack shapes below follow X's web client
// but are provisional: no recorded capture confirms them. A write is applied
// only with no errors and a captured ack, so until a capture sets
// ackCaptured, a response that matches the provisional ack is still
// ambiguous, and {"data":{}} is always ambiguous.

// listAck is how a list mutation's ack root confirms the change.
type listAck int

const (
	// ackNewList: the root is a list object whose id_str is the new list's
	// ID. An ack without a valid ID is ambiguous.
	ackNewList listAck = iota + 1
	// ackSameList: the root is a list object whose id_str equals the target
	// list.
	ackSameList
	// ackValue: the root holds a value (for example "Done").
	ackValue
)

// listWriteOp describes one typed list mutation.
type listWriteOp struct {
	method    string // SDK method name, reported as Operation
	operation string // GraphQL operation name, looked up in queryIDs
	ackRoot   string // provisional data field X returns when applied
	ack       listAck
	// ackCaptured reports that a recorded provider capture confirmed
	// ackRoot and ack. While it is false no response is applied.
	ackCaptured bool
}

var (
	createListOp       = listWriteOp{method: "CreateList", operation: "CreateList", ackRoot: "list", ack: ackNewList}
	updateListOp       = listWriteOp{method: "UpdateList", operation: "UpdateList", ackRoot: "list", ack: ackSameList}
	deleteListOp       = listWriteOp{method: "DeleteList", operation: "DeleteList", ackRoot: "list_delete", ack: ackValue}
	addListMemberOp    = listWriteOp{method: "AddListMember", operation: "ListAddMember", ackRoot: "list", ack: ackSameList}
	removeListMemberOp = listWriteOp{method: "RemoveListMember", operation: "ListRemoveMember", ackRoot: "list", ack: ackSameList}
	subscribeListOp    = listWriteOp{method: "SubscribeList", operation: "ListSubscribe", ackRoot: "list_subscribe_v3", ack: ackSameList}
	unsubscribeListOp  = listWriteOp{method: "UnsubscribeList", operation: "ListUnsubscribe", ackRoot: "list_unsubscribe_v3", ack: ackValue}
)

// listWriteOps lists every list mutation, for tests and documentation.
var listWriteOps = []listWriteOp{createListOp, updateListOp, deleteListOp, addListMemberOp, removeListMemberOp, subscribeListOp, unsubscribeListOp}

// validListInput reports whether in can be sent: a name that is not blank
// and valid UTF-8 text. The name and description length limits are not
// checked here; they wait for a Live capture.
func validListInput(in ListInput) bool {
	return strings.TrimSpace(in.Name) != "" && utf8.ValidString(in.Name) && utf8.ValidString(in.Description)
}

// CreateList creates an X list owned by the authenticated account. On
// success TargetID is the new list's ID; an ack without an ID is ambiguous.
func (c *Client) CreateList(ctx context.Context, in ListInput) (WriteResult, error) {
	op := createListOp
	if !validListInput(in) {
		return WriteResult{}, notAttempted(op.method, ErrInvalidParams)
	}
	return c.listWrite(ctx, op, "", map[string]interface{}{
		"isPrivate":   in.Private,
		"name":        in.Name,
		"description": in.Description,
	})
}

// UpdateList replaces the name, description and visibility of listID with
// in. TargetID is listID.
func (c *Client) UpdateList(ctx context.Context, listID string, in ListInput) (WriteResult, error) {
	op := updateListOp
	if !xNumericID(listID) || !validListInput(in) {
		return WriteResult{}, notAttempted(op.method, ErrInvalidParams)
	}
	return c.listWrite(ctx, op, listID, map[string]interface{}{
		"listId":      listID,
		"isPrivate":   in.Private,
		"name":        in.Name,
		"description": in.Description,
	})
}

// DeleteList deletes listID. TargetID is listID.
func (c *Client) DeleteList(ctx context.Context, listID string) (WriteResult, error) {
	return c.listOnly(ctx, deleteListOp, listID)
}

// AddListMember adds userID to listID. TargetID is listID.
func (c *Client) AddListMember(ctx context.Context, listID, userID string) (WriteResult, error) {
	return c.listMember(ctx, addListMemberOp, listID, userID)
}

// RemoveListMember removes userID from listID. TargetID is listID.
func (c *Client) RemoveListMember(ctx context.Context, listID, userID string) (WriteResult, error) {
	return c.listMember(ctx, removeListMemberOp, listID, userID)
}

// SubscribeList subscribes the authenticated account to listID. TargetID is
// listID.
func (c *Client) SubscribeList(ctx context.Context, listID string) (WriteResult, error) {
	return c.listOnly(ctx, subscribeListOp, listID)
}

// UnsubscribeList unsubscribes the authenticated account from listID.
// TargetID is listID.
func (c *Client) UnsubscribeList(ctx context.Context, listID string) (WriteResult, error) {
	return c.listOnly(ctx, unsubscribeListOp, listID)
}

func (c *Client) listOnly(ctx context.Context, op listWriteOp, listID string) (WriteResult, error) {
	if !xNumericID(listID) {
		return WriteResult{}, notAttempted(op.method, ErrInvalidParams)
	}
	return c.listWrite(ctx, op, listID, map[string]interface{}{"listId": listID})
}

func (c *Client) listMember(ctx context.Context, op listWriteOp, listID, userID string) (WriteResult, error) {
	if !xNumericID(listID) || !xNumericID(userID) {
		return WriteResult{}, notAttempted(op.method, ErrInvalidParams)
	}
	return c.listWrite(ctx, op, listID, map[string]interface{}{"listId": listID, "userId": userID})
}

// listWrite sends one list mutation and applies the ack rule. listID is the
// target list, or "" for CreateList.
func (c *Client) listWrite(ctx context.Context, op listWriteOp, listID string, vars map[string]interface{}) (WriteResult, error) {
	data, status, err := c.sendGraphQLWrite(ctx, op.method, op.operation, vars)
	if err != nil {
		return WriteResult{}, err
	}
	if !op.ackCaptured {
		return WriteResult{}, ambiguousWrite(op.method, status, 0, fmt.Errorf("%w: no recorded capture confirms the %s ack", errUnconfirmedWrite, op.operation))
	}
	target, err := op.confirm(data, listID)
	if err != nil {
		return WriteResult{}, ambiguousWrite(op.method, status, 0, err)
	}
	return WriteResult{Operation: op.method, TargetID: target, State: WriteApplied}, nil
}

// confirm checks data against the op's ack and returns the target ID.
func (op listWriteOp) confirm(data json.RawMessage, listID string) (string, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || op.ackRoot == "" || !nonEmptyJSON(fields[op.ackRoot]) {
		return "", errUnconfirmedWrite
	}
	root := fields[op.ackRoot]
	if op.ack == ackValue {
		return listID, nil
	}
	var list struct {
		IDStr string `json:"id_str"`
	}
	if json.Unmarshal(root, &list) != nil || !xNumericID(list.IDStr) {
		return "", fmt.Errorf("%w: response carried no list ID", errUnconfirmedWrite)
	}
	if op.ack == ackSameList && list.IDStr != listID {
		return "", fmt.Errorf("%w: response list does not match the target", errUnconfirmedWrite)
	}
	return list.IDStr, nil
}
