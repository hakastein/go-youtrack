package youtrack

// Code says what the caller does next about an error. The nine codes are the whole vocabulary; codes are added,
// never renamed.
type Code string

const (
	// CodeBadUsage is a call that cannot be sent as written, found before the write: fix the call. Reads may have
	// been made, the instance has not changed.
	CodeBadUsage Code = "bad_usage"
	// CodeUnknownName is a name found nowhere it was looked up: fix it by the nearest names or the candidates.
	CodeUnknownName Code = "unknown_name"
	// CodeMissingRequired is a write that leaves fields the project requires empty: fill every one named.
	CodeMissingRequired Code = "missing_required"
	// CodeNotFound is a 404 of the server: check the id.
	CodeNotFound Code = "not_found"
	// CodeDenied is a 401 or 403 of the server, or rights the token lacks: check the token and its rights.
	CodeDenied Code = "denied"
	// CodeRejected is a 400 of the server: read the upstream details; nothing was written.
	CodeRejected Code = "rejected"
	// CodeUpstreamFailed is a failure of the server or of the way to it: asking again may help.
	CodeUpstreamFailed Code = "upstream_failed"
	// CodeUpstreamInvalid is an answer that does not fit the request: asking again will not help.
	CodeUpstreamInvalid Code = "upstream_invalid"
	// CodeWriteUncertain is a write that left for the server with no answer on whether it went through: the
	// caller decides whether to write again.
	CodeWriteUncertain Code = "write_uncertain"
)

// Error is every error the module returns. Code says what to do next, Message says what happened, and Details carry
// the facts under keys of their own: the request as it can be sent again under request, the words of the server
// verbatim under upstream_status, upstream_error, upstream_message and upstream_body, the names at fault under
// unknown, missing or invalid. AfterWrite says the error came after the instance took a write. Err is the error of
// the transport under a request that got no answer or whose answer broke off.
type Error struct {
	Code       Code
	Message    string
	Details    []Pair
	AfterWrite bool
	Err        error
}

func (e *Error) Error() string {
	return string(e.Code) + ": " + e.Message
}

// Is matches an *Error of the same code, so errors.Is(err, ErrNotFound) holds for every not_found.
func (e *Error) Is(target error) bool {
	other, isError := target.(*Error)
	return isError && other.Code == e.Code
}

func (e *Error) Unwrap() error {
	return e.Err
}

// MayHaveWritten says the instance may have changed: the outcome of a write is unknown, or the error came after
// the instance took one.
func (e *Error) MayHaveWritten() bool {
	return e.Code == CodeWriteUncertain || e.AfterWrite
}

// Sentinels of the codes, for errors.Is.
var (
	ErrBadUsage        = &Error{Code: CodeBadUsage}
	ErrUnknownName     = &Error{Code: CodeUnknownName}
	ErrMissingRequired = &Error{Code: CodeMissingRequired}
	ErrNotFound        = &Error{Code: CodeNotFound}
	ErrDenied          = &Error{Code: CodeDenied}
	ErrRejected        = &Error{Code: CodeRejected}
	ErrUpstreamFailed  = &Error{Code: CodeUpstreamFailed}
	ErrUpstreamInvalid = &Error{Code: CodeUpstreamInvalid}
	ErrWriteUncertain  = &Error{Code: CodeWriteUncertain}
)

// Warning is what an operation noticed and went on past, in the shape of an Error: free text in a search, which
// YouTrack matches as words rather than reading as a condition.
type Warning struct {
	Code    Code
	Message string
	Details []Pair
}

// A nil *Error must come out as a nil error, not as an error holding a nil pointer.
func result[T any](value T, failed *Error) (T, error) {
	if failed != nil {
		var none T
		return none, failed
	}
	return value, nil
}

func optionsOf[T any](opts *T) T {
	if opts == nil {
		var defaults T
		return defaults
	}
	return *opts
}
