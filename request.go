package youtrack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const jsonContentType = "application/json"

type answer struct {
	request Request
	status  int
	body    []byte
	tree    any
	write   bool
}

func (c *Client) read(ctx context.Context, call func(ctx context.Context) (*http.Response, error)) (answer, error) {
	response, err := call(ctx)
	if err != nil {
		return answer{}, transportError(err, false)
	}
	defer response.Body.Close()
	a := answer{request: requestOf(response), status: response.StatusCode}
	if a.body, err = io.ReadAll(response.Body); err != nil {
		return answer{}, &TransportError{Request: a.request, Err: err}
	}
	tree, isJSON := decode(a.body)
	if !isJSON && bodyMustBeJSON(a.status) {
		return answer{}, a.invalid(notOneValue)
	}
	if a.status != http.StatusOK {
		return answer{}, a.statusError(tree)
	}
	a.tree = tree
	return a, nil
}

func (c *Client) send(ctx context.Context, call func(ctx context.Context) (*http.Response, error)) (answer, error) {
	response, err := Send(ctx, call)
	if err != nil {
		return answer{}, err
	}
	defer response.Body.Close()
	a := answer{request: requestOf(response), status: response.StatusCode, write: true}
	if a.body, err = io.ReadAll(response.Body); err != nil {
		return answer{}, &TransportError{Request: a.request, Err: err, Written: !statusRejectsWrite(a.status)}
	}
	tree, isJSON := decode(a.body)
	if a.status != http.StatusOK {
		if !isJSON && bodyMustBeJSON(a.status) {
			return answer{}, a.invalid(notOneValue)
		}
		return answer{}, a.statusError(tree)
	}
	if !isJSON {
		return answer{}, a.invalid(notOneValue)
	}
	a.tree = tree
	return a, nil
}

const notOneValue = "the answer is not one JSON value"

func (a answer) object() (map[string]any, error) {
	object, isObject := a.tree.(map[string]any)
	if !isObject {
		return nil, a.invalid("the answer is not a JSON object")
	}
	return object, nil
}

func (a answer) list() ([]map[string]any, error) {
	items, isList := a.tree.([]any)
	if !isList {
		return nil, a.invalid("the answer is not a JSON array of objects")
	}
	objects := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return nil, a.invalid("the answer is not a JSON array of objects")
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func (a answer) invalid(reason string) *ResponseError {
	return &ResponseError{Request: a.request, Status: a.status, Body: a.body, Reason: reason, Write: a.write}
}

func (a answer) statusError(tree any) *StatusError {
	failed := &StatusError{Request: a.request, Status: a.status, Body: a.body, Write: a.write}
	if said, isObject := tree.(map[string]any); isObject {
		failed.Code, _ = said["error"].(string)
		failed.Description, _ = said["error_description"].(string)
	}
	return failed
}

func transportError(err error, written bool) *TransportError {
	failed := &TransportError{Err: err, Written: written}
	var failedURL *url.Error
	if errors.As(err, &failedURL) {
		failed.Err = failedURL.Err
		failed.Request = Request{Method: strings.ToUpper(failedURL.Op), URL: readableURL(failedURL.URL)}
	}
	return failed
}

func requestOf(response *http.Response) Request {
	return Request{Method: response.Request.Method, URL: readableURL(response.Request.URL.Redacted())}
}

func readableURL(address string) string {
	path, query, split := strings.Cut(address, "?")
	if !split {
		return address
	}
	return path + "?" + strings.NewReplacer("%24", "$", "%28", "(", "%29", ")", "%2C", ",").Replace(query)
}

func is2xx(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

func is5xx(status int) bool {
	return status >= http.StatusInternalServerError && status < 600
}

func bodyMustBeJSON(status int) bool {
	return !is5xx(status)
}

func statusRejectsWrite(status int) bool {
	return !is2xx(status) && !is5xx(status)
}

const jsonWhitespace = " \t\r\n"

func decode(body []byte) (tree any, isJSON bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&tree) != nil || len(bytes.TrimLeft(body[decoder.InputOffset():], jsonWhitespace)) > 0 {
		return nil, false
	}
	return tree, true
}

func parseInt64(value any) (int64, bool) {
	number, isNumber := value.(json.Number)
	if !isNumber {
		return 0, false
	}
	count, err := number.Int64()
	if err != nil {
		return 0, false
	}
	return count, true
}
