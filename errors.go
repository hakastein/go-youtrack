package youtrack

import (
	"fmt"
	"strconv"
	"strings"
)

// Request is a request the way the caller may send it again: the method and the URL with a readable query.
type Request struct {
	Method string
	URL    string
}

func (r Request) String() string {
	return r.Method + " " + r.URL
}

// TransportError is a request that got no answer. Written says the request had left for the server before
// the failure, so a write may have gone through and the caller decides whether to repeat it.
type TransportError struct {
	Request Request
	Err     error
	Written bool
}

func (e *TransportError) Error() string {
	return e.Request.String() + ": " + e.Err.Error()
}

func (e *TransportError) Unwrap() error {
	return e.Err
}

// StatusError is an answer with a status other than 200. Code and Description carry the error and
// error_description of a YouTrack error body and are empty when the body is not one.
type StatusError struct {
	Request     Request
	Status      int
	Code        string
	Description string
	Body        []byte
	Write       bool
}

func (e *StatusError) Error() string {
	text := e.Request.String() + ": status " + strconv.Itoa(e.Status)
	if e.Code != "" {
		text += " " + e.Code
	}
	if e.Description != "" {
		text += ": " + e.Description
	}
	return text
}

// Uncertain says a write may have gone through: something other than YouTrack answered it with a 5xx.
func (e *StatusError) Uncertain() bool {
	return e.Write && is5xx(e.Status) && e.Code == ""
}

// Accepted says the server took a write and answered with a 2xx the module does not read.
func (e *StatusError) Accepted() bool {
	return e.Write && is2xx(e.Status)
}

// ResponseError is an answer that does not fit the request; sending the request again will not help.
// Write says the request was a write the server answered 200, so the instance holds it.
type ResponseError struct {
	Request Request
	Status  int
	Body    []byte
	Reason  string
	Write   bool
}

func (e *ResponseError) Error() string {
	return e.Request.String() + ": " + e.Reason
}

// ChangedFieldError is a custom field that is no longer the one its name resolved to between the two
// requests of a read; reading again may help.
type ChangedFieldError struct {
	Request Request
	Project string
	Field   string
	Body    []byte
}

func (e *ChangedFieldError) Error() string {
	return fmt.Sprintf("%s: the custom field %s of %s is no longer the one the name resolved to", e.Request, quote(e.Field), e.Project)
}

// PermissionError is a project that answered no custom fields, which is what the server sends a token
// without the right under Permission.
type PermissionError struct {
	Request    Request
	Project    string
	Permission string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("%s: not one custom field of %s arrived, and a token without %s is sent an empty list",
		e.Request, e.Project, e.Permission)
}

// MismatchError is a write that went through and came back holding something other than what was written.
type MismatchError struct {
	Request    Request
	Issue      string
	Mismatches []Mismatch
}

// Mismatch is one written field, with the values written and the values the answer holds, both by the
// value key of the field's type.
type Mismatch struct {
	Field    string
	Type     FieldType
	Expected []string
	Actual   []string
}

func (e *MismatchError) Error() string {
	fields := make([]string, 0, len(e.Mismatches))
	for _, m := range e.Mismatches {
		fields = append(fields, quote(m.Field))
	}
	return fmt.Sprintf("%s: %s came back as something other than what was written into %s",
		e.Request, strings.Join(fields, ", "), e.Issue)
}

// ArgumentError is an argument the call cannot send as written; the instance was not reached.
type ArgumentError struct {
	Argument string
	Value    string
	Reason   string
}

func (e *ArgumentError) Error() string {
	return e.Argument + " " + quote(e.Value) + " " + e.Reason
}

// FieldNameError is a call naming custom fields the project does not have, or names that more than one
// field of the project answers to. Known lists the names of the project's fields.
type FieldNameError struct {
	Request   Request
	Project   string
	Unknown   []string
	Ambiguous []AmbiguousName
	Known     []string
}

// AmbiguousName is a name that more than one custom field of the project answers to.
type AmbiguousName struct {
	Name       string
	Candidates []string
}

func (e *FieldNameError) Error() string {
	if len(e.Unknown) > 0 {
		return fmt.Sprintf("%s: %s are not custom fields of %s", e.Request, quoted(e.Unknown), e.Project)
	}
	names := make([]string, 0, len(e.Ambiguous))
	for _, a := range e.Ambiguous {
		names = append(names, a.Name)
	}
	return fmt.Sprintf("%s: %s are the names of more than one custom field of %s each", e.Request, quoted(names), e.Project)
}

// ValueError is a write holding values the fields they name cannot be given; nothing was sent.
type ValueError struct {
	Request Request
	Project string
	Invalid []InvalidValue
}

// InvalidValue is one value a field cannot be given and why.
type InvalidValue struct {
	Field  string
	Value  string
	Reason string
}

func (e *ValueError) Error() string {
	reasons := make([]string, 0, len(e.Invalid))
	for _, v := range e.Invalid {
		reasons = append(reasons, quote(v.Field)+"="+quote(v.Value)+": "+v.Reason)
	}
	return e.Request.String() + ": " + strings.Join(reasons, "; ")
}

// RequiredFieldError is a write that empties custom fields the project requires.
type RequiredFieldError struct {
	Request Request
	Project string
	Fields  []string
}

func (e *RequiredFieldError) Error() string {
	return fmt.Sprintf("%s: %s are required by %s and the call empties them", e.Request, quoted(e.Fields), e.Project)
}

func quote(text string) string {
	return strconv.Quote(text)
}

func quoted(names []string) string {
	all := make([]string, 0, len(names))
	for _, name := range names {
		all = append(all, quote(name))
	}
	return strings.Join(all, ", ")
}
