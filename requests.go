package youtrack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

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
	schemas      *schemas
	address      *url.URL
}

func (c *Client) read(ctx context.Context, spec *schemas, responseSchema string, requested []requestedField, call func(ctx context.Context, fields string) (*http.Response, error)) ([]*Node, *Error) {
	decoded, fault := c.request(ctx, spec, responseSchema, requested, call)
	if fault != nil {
		return nil, fault
	}
	return newConverter(decoded, blockLayout).objectsAt(decoded.schema, requested, decoded.objects)
}

type requestFields struct {
	sent   []requestedField
	output []requestedField
}

func (c *Client) readList(ctx context.Context, spec *schemas, responseSchema string, of requestFields, call func(ctx context.Context, fields string) (*http.Response, error)) ([]*Node, *Error) {
	decoded, fault := c.request(ctx, spec, responseSchema, of.sent, call)
	if fault != nil {
		return nil, fault
	}
	return newConverter(decoded, inlineLayout).objectsAt(decoded.schema, of.output, decoded.objects)
}

func writeEmpty(ctx context.Context, call func(ctx context.Context) (*http.Response, error)) *Error {
	response, fault := send(ctx, call)
	if fault != nil {
		return fault
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return truncatedWriteResponse(response, body, err)
	}
	if fault := writeFailure(response, body); fault != nil {
		return fault
	}
	if len(body) > 0 {
		message := "the answer carries a body, and this call is answered with none"
		return markWritten(response, shapeFailure(response, body, message))
	}
	return nil
}

func (c *Client) write(ctx context.Context, spec *schemas, responseSchema string, requested []requestedField, call func(ctx context.Context, fields string) (*http.Response, error), confirm func(decodedResponse) *Error, output func(decodedResponse) (*Node, *Error)) (*Node, *Error) {
	response, fault := send(ctx, func(ctx context.Context) (*http.Response, error) {
		return call(ctx, formatFields(requested))
	})
	if fault != nil {
		return nil, fault
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, truncatedWriteResponse(response, body, err)
	}
	if fault := writeFailure(response, body); fault != nil {
		return nil, fault
	}
	tree, isJSON := decode(body)
	if !isJSON {
		return nil, markWritten(response, shapeFailure(response, body, notOneValue))
	}
	decoded, fault := c.validateResponse(spec, responseSchema, requested, response, body, tree)
	if fault != nil {
		return nil, markWritten(response, fault)
	}
	if fault := confirm(decoded); fault != nil {
		return nil, markWritten(response, fault)
	}
	node, fault := output(decoded)
	if fault != nil {
		return nil, markWritten(response, fault)
	}
	return node, nil
}

func (c *Client) request(ctx context.Context, spec *schemas, responseSchema string, requested []requestedField, call func(ctx context.Context, fields string) (*http.Response, error)) (decodedResponse, *Error) {
	response, err := call(ctx, formatFields(requested))
	if err != nil {
		return decodedResponse{}, transportFailure(err, false)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return decodedResponse{}, readFailure(response, err)
	}
	tree, isJSON := decode(body)
	if !isJSON && bodyMustBeJSON(response.StatusCode) {
		return decodedResponse{}, shapeFailure(response, body, notOneValue)
	}
	if response.StatusCode != http.StatusOK {
		return decodedResponse{}, statusFailure(response, tree, body)
	}
	return c.validateResponse(spec, responseSchema, requested, response, body, tree)
}

const notOneValue = "the answer is not one JSON value"

func (c *Client) validateResponse(spec *schemas, responseSchema string, requested []requestedField, response *http.Response, body []byte, tree any) (decodedResponse, *Error) {
	expected := parseTypeRef(responseSchema)
	objects, ok := decodeObjects(tree, expected.list)
	switch {
	case !ok && expected.list:
		return decodedResponse{}, shapeFailure(response, body, "the answer is not a JSON array of objects")
	case !ok:
		return decodedResponse{}, shapeFailure(response, body, "the answer is not a JSON object")
	}
	if fault := checkMissingFields(spec, response, expected.schema, requested, tree); fault != nil {
		return decodedResponse{}, fault
	}
	return decodedResponse{objects: objects, httpResponse: response, body: body, schema: expected.schema, schemas: spec, address: c.address}, nil
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
