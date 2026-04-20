package x

import "errors"

var (
	ErrInvalidAuth   = errors.New("x: missing required cookie (auth_token or ct0)")
	ErrUnauthorized  = errors.New("x: authentication failed — session may be expired")
	ErrForbidden     = errors.New("x: access denied (protected account or private resource)")
	ErrNotFound      = errors.New("x: resource not found")
	ErrSuspended     = errors.New("x: account is suspended")
	ErrRateLimited   = errors.New("x: rate limited")
	ErrQueryIDStale  = errors.New("x: queryId is stale — use WithQueryIDs or RefreshQueryIDs")
	ErrInvalidParams = errors.New("x: invalid or missing required parameters")
	ErrPartialResult = errors.New("x: context cancelled; partial result returned")
	ErrRequestFailed = errors.New("x: HTTP request failed")

	ErrAlreadyRetweeted = errors.New("x: tweet already retweeted")
	ErrTweetTooLong     = errors.New("x: tweet text exceeds 280 characters")
	ErrDMClosed         = errors.New("x: recipient has DMs closed")
)
