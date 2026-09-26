package youtrack

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func readableQuery(query string) string {
	return strings.NewReplacer("%24", "$", "%28", "(", "%29", ")", "%2C", ",").Replace(query)
}

func is2xx(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

func is5xx(status int) bool {
	return status >= http.StatusInternalServerError && status < 600
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

func searchBody(query string) []byte {
	body, _ := json.Marshal(struct {
		Query string `json:"query"`
	}{Query: query})
	return body
}

type decodedResponse struct {
	objects      []map[string]any
	httpResponse *http.Response
	body         []byte
	schema       string
	spec         *schemas
	address      *url.URL
}

func sendWrite(ctx context.Context, call func(ctx context.Context) (*http.Response, error)) (*http.Response, []byte, *Error) {
	response, fault := send(ctx, call)
	if fault != nil {
		return nil, nil, fault
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, truncatedWriteResponse(response, body, err)
	}
	return response, body, writeFailure(response, body)
}

func writeEmpty(ctx context.Context, call func(ctx context.Context) (*http.Response, error)) *Error {
	response, body, fault := sendWrite(ctx, call)
	switch {
	case fault != nil:
		return fault
	case len(body) > 0:
		message := "the answer carries a body, and this call is answered with none"
		return markWritten(response, shapeFailure(response, body, message))
	}
	return nil
}

func writeAs[T any](ctx context.Context, c *Client, responseSchema string, requested []requestedField, call func(ctx context.Context, fields string) (*http.Response, error), confirm func(decodedResponse) *Error, output func(decodedResponse) (T, *Error)) (T, *Error) {
	var none T
	requested, fault := c.spec.askDurationsByMinutes(responseSchema, requested)
	if fault != nil {
		return none, fault
	}
	response, body, fault := sendWrite(ctx, func(ctx context.Context) (*http.Response, error) {
		return call(ctx, formatFields(requested))
	})
	if fault != nil {
		return none, fault
	}
	decoded, fault := c.decodeResponse(responseSchema, requested, response, body)
	if fault != nil {
		return none, markWritten(response, fault)
	}
	if fault := confirm(decoded); fault != nil {
		return none, markWritten(response, fault)
	}
	value, fault := output(decoded)
	if fault != nil {
		return none, markWritten(response, fault)
	}
	return value, nil
}

func (c *Client) request(ctx context.Context, responseSchema string, requested []requestedField, call func(ctx context.Context, fields string) (*http.Response, error)) (decodedResponse, *Error) {
	requested, fault := c.spec.askDurationsByMinutes(responseSchema, requested)
	if fault != nil {
		return decodedResponse{}, fault
	}
	response, err := call(ctx, formatFields(requested))
	if err != nil {
		return decodedResponse{}, transportFailure(err, false)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return decodedResponse{}, readFailure(response, err)
	}
	return c.decodeResponse(responseSchema, requested, response, body)
}

const notOneValue = "the answer is not one JSON value"

func (c *Client) decodeResponse(responseSchema string, requested []requestedField, response *http.Response, body []byte) (decodedResponse, *Error) {
	tree, isJSON := decode(body)
	if fault := answerFailure(response, body, tree, isJSON); fault != nil {
		return decodedResponse{}, fault
	}
	expected := parseTypeRef(responseSchema)
	objects, ok := decodeObjects(tree, expected.list)
	switch {
	case !ok && expected.list:
		return decodedResponse{}, shapeFailure(response, body, "the answer is not a JSON array of objects")
	case !ok:
		return decodedResponse{}, shapeFailure(response, body, "the answer is not a JSON object")
	}
	if fault := checkMissingFields(c.spec, response, expected.schema, requested, tree); fault != nil {
		return decodedResponse{}, fault
	}
	return decodedResponse{objects: objects, httpResponse: response, body: body, schema: expected.schema, spec: c.spec, address: c.address}, nil
}

func decodeObjects(tree any, isList bool) ([]map[string]any, bool) {
	items := []any{tree}
	if isList {
		list, ok := tree.([]any)
		if !ok {
			return nil, false
		}
		items = list
	}
	objects := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		objects = append(objects, object)
	}
	return objects, true
}
