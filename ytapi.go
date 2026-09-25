package youtrack

import (
	"bytes"
	"context"
	"net/http"

	"github.com/hakastein/youtrack/ytapi"
)

//go:generate go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.json

// API is the generated client over the transport of c: every operation of the YouTrack REST API, authorized
// with the token of c and sent once.
func (c *Client) API() *ytapi.Client {
	return &ytapi.Client{
		Server:         c.address.JoinPath("api/").String(),
		Client:         c.httpClient,
		RequestEditors: []ytapi.RequestEditorFn{c.authorize},
	}
}

func (c *Client) apiGetProject(ctx context.Context, code, fields string) (*http.Response, error) {
	return c.API().GetProject(ctx, code, &ytapi.GetProjectParams{Fields: &fields})
}

func (c *Client) apiGetProjectCustomField(ctx context.Context, code, id, fields string) (*http.Response, error) {
	return c.API().GetProjectCustomField(ctx, code, id, &ytapi.GetProjectCustomFieldParams{Fields: &fields})
}

func (c *Client) apiGetIssue(ctx context.Context, id, fields string) (*http.Response, error) {
	return c.API().GetIssue(ctx, id, &ytapi.GetIssueParams{Fields: &fields})
}

func (c *Client) apiUpdateIssue(ctx context.Context, id string, body []byte, fields string) (*http.Response, error) {
	return c.API().UpdateIssueWithBody(ctx, id, &ytapi.UpdateIssueParams{Fields: &fields}, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiGetUsers(ctx context.Context, query, fields string, top int32) (*http.Response, error) {
	return c.API().GetUsers(ctx, &ytapi.GetUsersParams{Fields: &fields, Top: &top, Query: &query})
}
