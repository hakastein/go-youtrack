package youtrack

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

func transportFailure(err error, written bool) *Error {
	fault := &Error{Code: CodeUpstreamFailed, Message: err.Error(), Err: err}
	if written {
		fault.Code = CodeWriteUncertain
	}
	var failed *url.Error
	if errors.As(err, &failed) {
		fault.Message = failed.Err.Error()
		fault.Err = failed.Err
		fault.Details = []Pair{requestDetail(strings.ToUpper(failed.Op), failed.URL)}
	}
	return fault
}

func readFailure(response *http.Response, err error) *Error {
	return &Error{Code: CodeUpstreamFailed, Message: err.Error(), Details: responseDetails(response), Err: err}
}

func truncatedWriteResponse(response *http.Response, body []byte, err error) *Error {
	fault := readFailure(response, err)
	if statusRejectsWrite(response.StatusCode) {
		return fault
	}
	fault.Code = CodeWriteUncertain
	fault.Details = append(fault.Details, bodyDetail(body))
	return fault
}

func writeFailure(response *http.Response, body []byte) *Error {
	if response.StatusCode == http.StatusOK {
		return nil
	}
	tree, isJSON := decode(body)
	answeredByProxy := is5xx(response.StatusCode) && !isYouTrackError(tree)
	if answeredByProxy {
		message := fmt.Sprintf("something other than YouTrack answered the write with status %d", response.StatusCode)
		details := append(responseDetails(response), bodyDetail(body))
		return &Error{Code: CodeWriteUncertain, Message: message, Details: details}
	}
	return markWritten(response, answerFailure(response, body, tree, isJSON))
}

func answerFailure(response *http.Response, body []byte, tree any, isJSON bool) *Error {
	switch {
	case !isJSON && !is5xx(response.StatusCode):
		return shapeFailure(response, body, notOneValue)
	case response.StatusCode != http.StatusOK:
		return statusFailure(response, tree, body)
	}
	return nil
}

func isYouTrackError(tree any) bool {
	said, isObject := tree.(map[string]any)
	if !isObject {
		return false
	}
	_, named := said["error"].(string)
	return named
}

func markWritten(response *http.Response, fault *Error) *Error {
	fault.AfterWrite = is2xx(response.StatusCode)
	return fault
}

func statusFailure(response *http.Response, tree any, body []byte) *Error {
	code, named := statusCode(response.StatusCode)
	details := responseDetails(response)
	said, _ := tree.(map[string]any)
	carried := 0
	for _, member := range []struct{ name, key string }{{"error", "upstream_error"}, {"error_description", "upstream_message"}} {
		if text, ok := said[member.name].(string); ok {
			details = append(details, Pair{Key: member.key, Value: NewString(text)})
			carried++
		}
	}
	if !named || said == nil || len(said) > carried {
		details = append(details, bodyDetail(body))
	}
	message := fmt.Sprintf("the server answered with status %d", response.StatusCode)
	return &Error{Code: code, Message: message, Details: details}
}

func shapeFailure(response *http.Response, body []byte, message string) *Error {
	return &Error{Code: CodeUpstreamInvalid, Message: message, Details: append(responseDetails(response), bodyDetail(body))}
}

func statusCode(status int) (code Code, named bool) {
	switch {
	case status == http.StatusBadRequest:
		return CodeRejected, true
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return CodeDenied, true
	case status == http.StatusNotFound:
		return CodeNotFound, true
	case is5xx(status):
		return CodeUpstreamFailed, true
	}
	return CodeUpstreamFailed, false
}

func bodyDetail(body []byte) Pair {
	return Pair{Key: "upstream_body", Value: NewString(string(body))}
}

func responseDetails(response *http.Response) []Pair {
	return []Pair{
		sentRequest(response),
		{Key: "upstream_status", Value: intNode(response.StatusCode)},
	}
}

func sentRequest(response *http.Response) Pair {
	return requestDetail(response.Request.Method, response.Request.URL.Redacted())
}

func (a decodedResponse) sent() Pair {
	return sentRequest(a.httpResponse)
}

func (a decodedResponse) invalid(message string) *Error {
	return shapeFailure(a.httpResponse, a.body, message)
}

func (a decodedResponse) fault(code Code, message string, details ...Pair) *Error {
	return &Error{Code: code, Message: message, Details: append([]Pair{a.sent()}, details...)}
}

func requestDetail(method, address string) Pair {
	if path, query, split := strings.Cut(address, "?"); split {
		address = path + "?" + readableQuery(query)
	}
	return Pair{Key: requestKey, Value: NewString(method + " " + address)}
}

const requestKey = "request"

func insertAfterRequest(details []Pair, own ...Pair) []Pair {
	at := slices.IndexFunc(details, func(pair Pair) bool { return pair.Key == requestKey })
	return slices.Insert(slices.Clone(details), at+1, own...)
}
