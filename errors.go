package x

import (
	"errors"
	"fmt"
)

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
	ErrTweetTooLong     = errors.New("x: tweet text exceeds account character limit")
	ErrDMClosed         = errors.New("x: recipient has DMs closed")
	ErrChallenge        = errors.New("x: additional verification required")
	ErrDuplicatePost    = errors.New("x: post duplicates a recent post")
	ErrAutomatedRequest = errors.New("x: request was flagged as automated")
	ErrReplyRestricted  = errors.New("x: replies to this post are restricted or it is not visible")
	ErrDailyPostLimit   = errors.New("x: account reached its daily post limit")
	// ErrPostingLimited is X code 344. Public references disagree on whether
	// 344 is a short network-level posting throttle or the daily limit, and
	// no recorded X body settles it, so a 344 also matches ErrDailyPostLimit:
	// callers that pause for the daily limit stay conservative, and callers
	// that check ErrPostingLimited first can choose a shorter pause.
	ErrPostingLimited = errors.New("x: posting is temporarily limited")
	// ErrMediaRejected is a definite media rejection: an invalid, expired or
	// unknown media ID, or a disallowed media combination (323, 324, 325, 386).
	ErrMediaRejected     = errors.New("x: media was rejected")
	ErrDefinite          = errors.New("x: request definitely did not complete")
	ErrAmbiguous         = errors.New("x: request outcome is ambiguous")
	errWriteNotAttempted = errors.New("x: write was not attempted")
	errMissingAck        = errors.New("x: write response carried no post ID")
	// ErrQueryIDUnavailable means the SDK has no GraphQL query id for the
	// operation a method needs, so it sent nothing. Query ids are added only
	// from recorded provider captures.
	ErrQueryIDUnavailable = errors.New("x: no query id is available for this operation")
	errRedirectRefused    = errors.New("x: provider redirect refused")
	errUnconfirmedWrite   = errors.New("x: write response did not confirm the change")

	ErrUnsupportedMediaType = errors.New("x: unsupported media type")
	ErrMediaTooLarge        = errors.New("x: media exceeds the maximum allowed size")

	ErrMediaProcessingFailed  = errors.New("x: media processing failed")
	ErrMediaProcessingTimeout = errors.New("x: media processing did not complete in time")
)

// OperationError retains bounded, non-secret evidence about a failed provider
// operation. It deliberately excludes request URLs, query IDs, variables,
// response bodies, cookies, identities, and proxy details.
type OperationError struct {
	Operation              string
	Status                 int
	ContentType            string
	TransactionIDAttached  bool
	TransactionReady       bool
	QueryMetadataRefreshed bool
	Err                    error
}

func (e *OperationError) Error() string {
	return fmt.Sprintf("x: %s failed: %v", e.Operation, e.Err)
}

func (e *OperationError) Unwrap() error { return e.Err }

// OutcomeError classifies whether a failed write definitely did not complete
// or may have completed before the transport failed.
//
// Outcome is ErrDefinite or ErrAmbiguous. The typed write methods (LikePost,
// FollowUser, DeletePost, ...) also fill the remaining fields; CreatePost,
// QuotePost and ReplyToPost set only Outcome and Err.
type OutcomeError struct {
	Outcome error
	Err     error

	// Operation is the SDK method that failed, for example "LikePost".
	Operation string
	// Status is the provider HTTP status, or 0 when no response arrived.
	Status int
	// Code is the first X error code in the response, or 0.
	Code int
	// NotAttempted reports that no request was sent (invalid input, no query
	// id, or a context that ended before sending). It implies ErrDefinite.
	NotAttempted bool
	// Ambiguous reports that the write may have been applied. It matches
	// Outcome == ErrAmbiguous on the typed write methods.
	Ambiguous bool
}

// outcome is Outcome, or the sentinel the flags imply when Outcome is nil.
func (e *OutcomeError) outcome() error {
	switch {
	case e.Outcome != nil:
		return e.Outcome
	case e.Ambiguous:
		return ErrAmbiguous
	case e.NotAttempted:
		return ErrDefinite
	default:
		return nil
	}
}

func (e *OutcomeError) Error() string {
	msg := "x: write failed"
	if outcome := e.outcome(); outcome != nil {
		msg = outcome.Error()
	}
	if e.Err == nil {
		return msg
	}
	return msg + ": " + e.Err.Error()
}

func (e *OutcomeError) Unwrap() []error {
	errs := make([]error, 0, 2)
	if outcome := e.outcome(); outcome != nil {
		errs = append(errs, outcome)
	}
	if e.Err != nil {
		errs = append(errs, e.Err)
	}
	return errs
}

func classifyWriteOutcome(err error) error {
	if err == nil {
		return nil
	}
	var classified *OutcomeError
	if errors.As(err, &classified) {
		return err
	}
	outcome := ErrAmbiguous
	if errors.Is(err, ErrInvalidParams) ||
		errors.Is(err, ErrUnauthorized) ||
		errors.Is(err, ErrForbidden) ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrSuspended) ||
		errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrChallenge) ||
		errors.Is(err, ErrDMClosed) ||
		errors.Is(err, ErrTweetTooLong) ||
		errors.Is(err, ErrDuplicatePost) ||
		errors.Is(err, ErrAutomatedRequest) ||
		errors.Is(err, ErrReplyRestricted) ||
		errors.Is(err, ErrDailyPostLimit) ||
		errors.Is(err, ErrMediaRejected) ||
		errors.Is(err, ErrQueryIDStale) ||
		errors.Is(err, errWriteNotAttempted) {
		outcome = ErrDefinite
	}
	return &OutcomeError{Outcome: outcome, Err: err}
}

// classifyXErrorCode maps X's numeric error codes, shared by GraphQL and REST
// error bodies, to sentinels. It returns nil for codes without a mapping so
// callers can fall back to message matching.
func classifyXErrorCode(code int) error {
	switch code {
	case 32:
		return ErrUnauthorized
	case 34, 144:
		return ErrNotFound
	case 63:
		return ErrSuspended
	case 88:
		return ErrRateLimited
	case 185:
		return ErrDailyPostLimit
	case 186:
		return ErrTweetTooLong
	case 187:
		return ErrDuplicatePost
	case 214:
		return ErrInvalidParams
	case 226:
		return ErrAutomatedRequest
	case 323, 324, 325, 386:
		return ErrMediaRejected
	case 326:
		return ErrChallenge
	case 327:
		return ErrAlreadyRetweeted
	case 344:
		return errPostingLimited
	case 349:
		return ErrDMClosed
	case 385, 433:
		return ErrReplyRestricted
	default:
		return nil
	}
}

// errPostingLimited is what code 344 maps to: it matches both
// ErrPostingLimited and ErrDailyPostLimit (see ErrPostingLimited).
var errPostingLimited error = sentinelSet{ErrPostingLimited, ErrDailyPostLimit}

// sentinelSet is an error that matches every sentinel it lists; its message is
// the first one's.
type sentinelSet []error

func (s sentinelSet) Error() string   { return s[0].Error() }
func (s sentinelSet) Unwrap() []error { return s }
