package x

import (
	"context"
	"encoding/json"
	"sync/atomic"
)

// PageEnd says how a timeline page ends. HasNext on TweetPage, UserPage and
// ListPage keeps its older meaning (X sent a Bottom cursor); PageEnd says what
// that cursor, or its absence, is worth.
//
// The states are decided in this order:
//
//   - PageEndStalled: the page's Bottom cursor equals the cursor the request
//     sent, so following it cannot advance. Only the *Page methods that know
//     the request cursor report it (see the method docs).
//   - PageEndVerified: no Bottom cursor and a terminal instruction
//     (TimelineTerminateTimeline, direction Bottom) on an operation whose
//     terminal shape a Live capture has proven. No operation is proven yet,
//     so no page reports it today.
//   - PageEndEmpty: the page carried no items and dropped none, with or
//     without a Bottom cursor.
//   - PageEndMore: a Bottom cursor.
//   - PageEndUnverified: no Bottom cursor. A missing continuation is not
//     proof that the collection ended.
type PageEnd string

const (
	PageEndMore       PageEnd = "more"
	PageEndUnverified PageEnd = "end_unverified"
	PageEndVerified   PageEnd = "end_verified"
	PageEndStalled    PageEnd = "stalled"
	PageEndEmpty      PageEnd = "empty"
)

// PageCoverage reports how complete one page is.
type PageCoverage struct {
	End PageEnd `json:"end"`
	// Dropped counts items X sent that could not be read (a tombstone, an
	// unavailable user or post, an item without an ID).
	Dropped int `json:"dropped"`
	// PartialErrors counts the GraphQL errors X reported beside usable data
	// (kind NonFatal, or a path below the root field) on the request that
	// produced the page. Only the *Page methods that say so count them; the
	// Parse* functions leave it 0.
	PartialErrors int `json:"partialErrors"`
}

// terminalProven lists the GraphQL operations whose terminal instruction a
// Live capture has proven. Only these may report PageEndVerified. It is empty
// until such a capture exists.
var terminalProven = map[string]bool{}

// pageEnd is the coverage a parser can decide from the page alone.
func pageEnd(items, dropped int, nextCursor string) PageEnd {
	switch {
	case items == 0 && dropped == 0:
		return PageEndEmpty
	case nextCursor != "":
		return PageEndMore
	default:
		return PageEndUnverified
	}
}

// hasTweetResult reports whether an entry carried a post slot at all, so a
// slot that yields no post counts as dropped.
func hasTweetResult(ic *itemContent) bool {
	return ic != nil && ic.TweetResults != nil
}

// pageRequest holds what only the caller of a page read knows: the operation,
// its timeline path, the cursor it sent and the partial errors X reported.
type pageRequest struct {
	operation string
	timeline  string
	cursor    string
	partial   *atomic.Int64
}

// newPageRequest prepares a page read and returns the context that counts
// partial GraphQL errors for it.
func newPageRequest(ctx context.Context, operation, timeline, cursor string) (context.Context, pageRequest) {
	r := pageRequest{operation: operation, timeline: timeline, cursor: cursor, partial: new(atomic.Int64)}
	return context.WithValue(ctx, partialErrorsKey{}, r.partial), r
}

// finish completes cov with the facts the parser could not see.
func (r pageRequest) finish(cov *PageCoverage, raw json.RawMessage, nextCursor string) {
	cov.PartialErrors = int(r.partial.Load())
	switch {
	case nextCursor != "" && nextCursor == r.cursor:
		cov.End = PageEndStalled
	case nextCursor == "" && terminalProven[r.operation] && bottomTerminated(raw, r.timeline):
		cov.End = PageEndVerified
	}
}

// tweetPage parses a tweet page read under r and completes its coverage.
func (r pageRequest) tweetPage(raw json.RawMessage) (TweetPage, error) {
	page, err := parseTweetPage(raw, r.timeline)
	if err != nil {
		return TweetPage{}, err
	}
	r.finish(&page.Coverage, raw, page.NextCursor)
	return page, nil
}

// userPage parses a user page read under r and completes its coverage.
func (r pageRequest) userPage(raw json.RawMessage) (UserPage, error) {
	page, err := parseUserPage(raw, r.timeline)
	if err != nil {
		return UserPage{}, err
	}
	r.finish(&page.Coverage, raw, page.NextCursor)
	return page, nil
}

// listPage parses a list page read under r and completes its coverage.
func (r pageRequest) listPage(raw json.RawMessage) (ListPage, error) {
	page, err := parseListPage(raw, r.timeline)
	if err != nil {
		return ListPage{}, err
	}
	r.finish(&page.Coverage, raw, page.NextCursor)
	return page, nil
}

// bottomTerminated reports whether the timeline carries a Bottom
// TimelineTerminateTimeline instruction.
func bottomTerminated(raw json.RawMessage, timeline string) bool {
	instructions, err := extractInstructions(raw, timeline)
	if err != nil {
		return false
	}
	for _, inst := range instructions {
		if inst.Type == "TimelineTerminateTimeline" && inst.Direction == "Bottom" {
			return true
		}
	}
	return false
}

// partialErrorsKey carries a request-scoped counter of partial GraphQL
// errors, in the manner of httptrace: doGraphQLGET adds to it when it accepts
// data that came with partial errors.
type partialErrorsKey struct{}

func notePartialErrors(ctx context.Context, envelope gqlResponse) {
	counter, _ := ctx.Value(partialErrorsKey{}).(*atomic.Int64)
	if counter == nil {
		return
	}
	counter.Add(int64(len(envelope.Errors)))
}
