package emailvalidation

import (
	"errors"
	"strconv"
	"strings"
)

// ErrorKind is the family of an *Error. Branch on the kind (or on Code), never on Message.
type ErrorKind string

// Error kinds. Use errors.Is with the matching sentinel (ErrRateLimited, …) or compare (*Error).Kind.
const (
	// KindEmailValidation is used directly only for a 2xx answer whose body is not JSON (unexpected_response).
	KindEmailValidation ErrorKind = "EmailValidationError"
	// KindInvalidRequest: 400, 411, 413, any other 4xx without its own kind, a call refused before sending
	// (status 0), or a range download answered without 206.
	KindInvalidRequest ErrorKind = "InvalidRequestError"
	// KindAuthentication: 401, a missing, malformed, revoked or rolled key.
	KindAuthentication ErrorKind = "AuthenticationError"
	// KindInsufficientCredits: 402, not enough credits. Nothing was charged.
	KindInsufficientCredits ErrorKind = "InsufficientCreditsError"
	// KindPermissionDenied: 403, missing scope, disallowed origin, suspended account or billing hold.
	KindPermissionDenied ErrorKind = "PermissionDeniedError"
	// KindNotFound: 404, no such resource for this account.
	KindNotFound ErrorKind = "NotFoundError"
	// KindConflict: 409, the resource is in the wrong state.
	KindConflict ErrorKind = "ConflictError"
	// KindRateLimit: 429. Retried automatically when the wait is short, except key_daily_cap.
	KindRateLimit ErrorKind = "RateLimitError"
	// KindServiceUnavailable: 503, temporarily unavailable, nothing charged. Retried automatically.
	KindServiceUnavailable ErrorKind = "ServiceUnavailableError"
	// KindServer: 500, any other status >= 500 and any other non-2xx status (such as a redirect).
	KindServer ErrorKind = "ServerError"
	// KindConnection: no HTTP answer (connection_error, timeout), the caller cancelled (aborted), or the
	// Bulk.Wait deadline passed (timeout).
	KindConnection ErrorKind = "ConnectionError"
)

// Error is the one error type of every API call. Branch on Kind (or errors.Is with a sentinel) or on Code, never on
// Message.
type Error struct {
	Kind ErrorKind
	// Status is the HTTP status, or 0 when no answer arrived or the call was refused before sending.
	Status int
	// Code is an API error code (CodeRateLimited, …) or a client code (CodeConnectionError, CodeTimeout,
	// CodeUnexpectedResponse, CodeAborted).
	Code ErrorCode
	// Message is for people and may change.
	Message string
	// RequestID is our id for the request (quote it to support), or "" when there is none.
	RequestID string
	// RetryAfter is the whole seconds from the Retry-After header, or nil.
	RetryAfter *int
	// Err is the underlying cause, if any (for example context.Canceled). It never contains the API key.
	Err error
}

// Error shows the kind, status, code, request id and message. It never includes the API key, the request body or
// the checked address.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("emailvalidation: ")
	b.WriteString(string(e.Kind))
	b.WriteString(" (status ")
	b.WriteString(strconv.Itoa(e.Status))
	b.WriteString(", code ")
	b.WriteString(string(e.Code))
	if e.RequestID != "" {
		b.WriteString(", request_id ")
		b.WriteString(e.RequestID)
	}
	b.WriteString("): ")
	b.WriteString(e.Message)
	return b.String()
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error { return e.Err }

// Is matches the sentinel of the error's kind, so errors.Is(err, ErrRateLimited) works.
func (e *Error) Is(target error) bool {
	s, ok := target.(*kindError)
	return ok && s.kind == e.Kind
}

type kindError struct {
	kind ErrorKind
	text string
}

func (k *kindError) Error() string { return k.text }

// Sentinels per kind, for errors.Is. errors.As(err, &apiErr) with apiErr *Error gives every field.
var (
	ErrInvalidRequest      error = &kindError{KindInvalidRequest, "emailvalidation: invalid request"}
	ErrAuthentication      error = &kindError{KindAuthentication, "emailvalidation: authentication failed"}
	ErrInsufficientCredits error = &kindError{KindInsufficientCredits, "emailvalidation: insufficient credits"}
	ErrPermissionDenied    error = &kindError{KindPermissionDenied, "emailvalidation: permission denied"}
	ErrNotFound            error = &kindError{KindNotFound, "emailvalidation: not found"}
	ErrConflict            error = &kindError{KindConflict, "emailvalidation: conflict"}
	ErrRateLimited         error = &kindError{KindRateLimit, "emailvalidation: rate limited"}
	ErrServiceUnavailable  error = &kindError{KindServiceUnavailable, "emailvalidation: service unavailable"}
	ErrServer              error = &kindError{KindServer, "emailvalidation: server error"}
	ErrConnection          error = &kindError{KindConnection, "emailvalidation: connection error"}
	// ErrUnexpectedResponse matches the KindEmailValidation error of a 2xx answer that is not JSON.
	ErrUnexpectedResponse error = &kindError{KindEmailValidation, "emailvalidation: unexpected response"}
)

// ErrInvalidArgument is wrapped by every error the SDK returns for a wrong argument or option before any request
// (an empty API key, a base URL that is not http(s), both or neither of Emails and File, …).
var ErrInvalidArgument = errors.New("emailvalidation: invalid argument")

func argError(text string) error {
	return &argumentError{text}
}

type argumentError struct{ text string }

func (a *argumentError) Error() string { return "emailvalidation: invalid argument: " + a.text }
func (a *argumentError) Unwrap() error { return ErrInvalidArgument }

// WebhookVerificationError is returned by VerifyWebhook (and SignWebhook for a bad secret). It is not an *Error.
type WebhookVerificationError struct {
	// Message is one of the fixed texts, e.g. "No webhook signature matched.".
	Message string
}

func (e *WebhookVerificationError) Error() string { return e.Message }

// Messages of the SDK's own errors.
const (
	msgNotJSON      = "The API answered with something other than JSON."
	msgUnreachable  = "The API could not be reached."
	msgTimedOut     = "The request timed out."
	msgAborted      = "The request was aborted."
	msgBadJobID     = "Not a bulk job id (expected job_ followed by 22 letters and digits)."
	msgRangeRefused = "This download cannot be fetched in parts: use Filter \"all\" (or one status with " +
		"EscapeFormulas false) and Columns \"original\" with a Range."
)

func kindForStatus(status int) ErrorKind {
	switch status {
	case 400, 411, 413:
		return KindInvalidRequest
	case 401:
		return KindAuthentication
	case 402:
		return KindInsufficientCredits
	case 403:
		return KindPermissionDenied
	case 404:
		return KindNotFound
	case 409:
		return KindConflict
	case 429:
		return KindRateLimit
	case 503:
		return KindServiceUnavailable
	}
	if status >= 400 && status < 500 {
		return KindInvalidRequest
	}
	return KindServer
}
