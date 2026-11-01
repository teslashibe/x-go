package x

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrQueryIDUnavailable means the client holds no GraphQL query id for the
// operation, so the method refused before sending any request. Query ids for
// the collection reads below enter the defaults only from a Live capture;
// until then a caller may supply one with WithQueryIDs or RefreshQueryIDs.
var ErrQueryIDUnavailable = errors.New("x: no query id is available for this operation")

// QueryIDUnavailableError names the operation that has no query id. It
// matches ErrQueryIDUnavailable.
type QueryIDUnavailableError struct {
	Operation string
}

func (e *QueryIDUnavailableError) Error() string {
	return "x: no query id is available for " + e.Operation
}

// Is reports whether target is ErrQueryIDUnavailable.
func (e *QueryIDUnavailableError) Is(target error) bool { return target == ErrQueryIDUnavailable }

// ListPage is one page of lists with a cursor for the next page.
type ListPage struct {
	Lists      []List       `json:"lists"`
	NextCursor string       `json:"nextCursor,omitempty"`
	HasNext    bool         `json:"hasNext"`
	Coverage   PageCoverage `json:"coverage"`
}

// collectionOp is one collection read. The operation names, variables and
// response paths follow X's web client, but none has a Live capture yet: no
// query id for them ships in defaultQueryIDs, and the paths are confirmed or
// corrected together with the query id when the capture exists.
type collectionOp struct {
	name     string
	timeline string
}

var (
	opBookmarks         = collectionOp{name: "Bookmarks", timeline: "bookmark_timeline_v2.timeline"}
	opLikes             = collectionOp{name: "Likes", timeline: "user.result.timeline"}
	opMutedAccounts     = collectionOp{name: "MutedAccounts", timeline: "viewer.muting_timeline"}
	opBlockedAccounts   = collectionOp{name: "BlockedAccountsAll", timeline: "viewer.timeline"}
	opListOwnerships    = collectionOp{name: "ListOwnerships", timeline: "user.result.timeline"}
	opListSubscriptions = collectionOp{name: "ListSubscriptions", timeline: "viewer.list_subscriptions_timeline"}
)

// GetBookmarksPage returns a page of the authenticated account's bookmarks.
//
// Like every collection read below it sends at most one request and never
// retries: a throttle returns *RateLimitError (the cooldown is recorded), and
// a non-JSON HTTP 404 returns an *OperationError matching ErrQueryIDStale
// rather than ErrNotFound. Without a query id it returns
// *QueryIDUnavailableError and sends nothing. Coverage reports stalled,
// dropped items and partial errors.
func (c *Client) GetBookmarksPage(ctx context.Context, count int, cursor string) (TweetPage, error) {
	vars := collectionVars(count, cursor)
	raw, req, err := c.collectionGET(ctx, opBookmarks, cursor, vars)
	if err != nil {
		return TweetPage{}, err
	}
	return req.tweetPage(raw)
}

// GetLikedPostsPage returns a page of the posts userID liked. X shows likes
// only for the authenticated account. See GetBookmarksPage for the request
// and error rules.
func (c *Client) GetLikedPostsPage(ctx context.Context, userID string, count int, cursor string) (TweetPage, error) {
	if userID == "" {
		return TweetPage{}, fmt.Errorf("%w: userID must not be empty", ErrInvalidParams)
	}
	vars := collectionVars(count, cursor)
	vars["userId"] = userID
	raw, req, err := c.collectionGET(ctx, opLikes, cursor, vars)
	if err != nil {
		return TweetPage{}, err
	}
	return req.tweetPage(raw)
}

// GetMutedUsersPage returns a page of the accounts the authenticated account
// mutes. See GetBookmarksPage for the request and error rules.
func (c *Client) GetMutedUsersPage(ctx context.Context, count int, cursor string) (UserPage, error) {
	return c.collectionUserPage(ctx, opMutedAccounts, count, cursor)
}

// GetBlockedUsersPage returns a page of the accounts the authenticated
// account blocks. See GetBookmarksPage for the request and error rules.
func (c *Client) GetBlockedUsersPage(ctx context.Context, count int, cursor string) (UserPage, error) {
	return c.collectionUserPage(ctx, opBlockedAccounts, count, cursor)
}

// GetOwnedListsPage returns a page of the lists userID owns. See
// GetBookmarksPage for the request and error rules, and List for the fields
// that stay unmapped until a Live capture.
func (c *Client) GetOwnedListsPage(ctx context.Context, userID string, count int, cursor string) (ListPage, error) {
	if userID == "" {
		return ListPage{}, fmt.Errorf("%w: userID must not be empty", ErrInvalidParams)
	}
	vars := collectionVars(count, cursor)
	vars["userId"] = userID
	vars["isListMemberTargetUserId"] = userID
	return c.collectionListPage(ctx, opListOwnerships, cursor, vars)
}

// GetSubscribedListsPage returns a page of the lists the authenticated
// account subscribes to. See GetOwnedListsPage.
func (c *Client) GetSubscribedListsPage(ctx context.Context, count int, cursor string) (ListPage, error) {
	return c.collectionListPage(ctx, opListSubscriptions, cursor, collectionVars(count, cursor))
}

func (c *Client) collectionUserPage(ctx context.Context, op collectionOp, count int, cursor string) (UserPage, error) {
	raw, req, err := c.collectionGET(ctx, op, cursor, collectionVars(count, cursor))
	if err != nil {
		return UserPage{}, err
	}
	return req.userPage(raw)
}

func (c *Client) collectionListPage(ctx context.Context, op collectionOp, cursor string, vars map[string]interface{}) (ListPage, error) {
	raw, req, err := c.collectionGET(ctx, op, cursor, vars)
	if err != nil {
		return ListPage{}, err
	}
	return req.listPage(raw)
}

func collectionVars(count int, cursor string) map[string]interface{} {
	if count <= 0 {
		count = 20
	}
	vars := map[string]interface{}{
		"count":                  count,
		"includePromotedContent": false,
	}
	if cursor != "" {
		vars["cursor"] = cursor
	}
	return vars
}

// collectionGET sends exactly one GraphQL GET for op, or none when the client
// has no query id for it.
func (c *Client) collectionGET(ctx context.Context, op collectionOp, cursor string, vars map[string]interface{}) (json.RawMessage, pageRequest, error) {
	qid := c.queryID(op.name)
	if qid == "" {
		return nil, pageRequest{}, &QueryIDUnavailableError{Operation: op.name}
	}
	varsJSON, err := json.Marshal(vars)
	if err != nil {
		return nil, pageRequest{}, fmt.Errorf("%w: marshalling variables: %v", ErrInvalidParams, err)
	}
	featsJSON, err := json.Marshal(c.features)
	if err != nil {
		return nil, pageRequest{}, fmt.Errorf("%w: marshalling features: %v", ErrInvalidParams, err)
	}
	ctx, req := newPageRequest(ctx, op.name, op.timeline, cursor)
	raw, err := c.doGraphQLGET(ctx, qid, op.name, varsJSON, featsJSON)
	if err != nil {
		return nil, pageRequest{}, staleQueryID(err)
	}
	return raw, req, nil
}

// staleQueryID turns a non-JSON HTTP 404 into ErrQueryIDStale, keeping the
// operation evidence. A collection of the authenticated account has no
// target that could be absent, and X answers an unknown query id with a bare
// 404; a JSON 404 (for example code 34) keeps its classification.
func staleQueryID(err error) error {
	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.Status != http.StatusNotFound || opErr.ContentType == "application/json" {
		return err
	}
	stale := *opErr
	stale.Err = ErrQueryIDStale
	return &stale
}

// ---------------------------------------------------------------------------
// List objects
// ---------------------------------------------------------------------------

// listObj is the list object as ListBySlug returns it. List timelines are
// read with the same shape until a Live capture shows theirs.
type listObj struct {
	ID          string `json:"id_str"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	MemberCount int    `json:"member_count"`
	Mode        string `json:"mode"`
	User        struct {
		RestID string     `json:"rest_id"`
		Legacy userLegacy `json:"legacy"`
	} `json:"user"`
}

func toList(o listObj) List {
	return List{
		ID:              o.ID,
		Name:            o.Name,
		Slug:            o.Slug,
		Description:     o.Description,
		MemberCount:     o.MemberCount,
		OwnerID:         o.User.RestID,
		OwnerName:       o.User.Legacy.Name,
		OwnerScreenName: o.User.Legacy.ScreenName,
		IsPrivate:       o.Mode == "Private",
	}
}

// parseListPage extracts lists and cursor from a timeline instruction
// response, including lists grouped in module entries.
func parseListPage(raw json.RawMessage, timelineKey string) (ListPage, error) {
	instructions, err := extractInstructions(raw, timelineKey)
	if err != nil {
		return ListPage{}, err
	}

	var page ListPage
	add := func(o *listObj) {
		if o == nil {
			return
		}
		if o.ID == "" {
			page.Coverage.Dropped++
			return
		}
		page.Lists = append(page.Lists, toList(*o))
	}
	for _, inst := range instructions {
		entries := inst.Entries
		if inst.Entry != nil {
			entries = append(entries, *inst.Entry)
		}
		for _, entry := range entries {
			if ic := entry.Content.ItemContent; ic != nil {
				add(ic.List)
			}
			if entry.Content.CursorType == "Bottom" && entry.Content.Value != "" {
				page.NextCursor = entry.Content.Value
				page.HasNext = true
			}
			for _, item := range entry.Content.Items {
				add(item.Item.ItemContent.List)
			}
		}
	}

	page.Coverage.End = pageEnd(len(page.Lists), page.Coverage.Dropped, page.NextCursor)
	return page, nil
}
