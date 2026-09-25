package youtrack

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/hakastein/youtrack/ytapi"
)

//go:generate go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.json

const jsonContentType = "application/json"

// API is the generated client over the transport of c: every operation of the YouTrack REST API, authorized
// with the token of c and sent once. A write through it goes through Send.
func (c *Client) API() *ytapi.Client {
	return &ytapi.Client{
		Server:         c.address.JoinPath("api/").String(),
		Client:         c.httpClient,
		RequestEditors: []ytapi.RequestEditorFn{c.authorize},
	}
}

func (c *Client) api() *ytapi.Client {
	return c.API()
}

func (c *Client) apiGetProject(ctx context.Context, code, fields string) (*http.Response, error) {
	return c.api().GetProject(ctx, code, &ytapi.GetProjectParams{Fields: &fields})
}

func (c *Client) apiGetProjects(ctx context.Context, fields string, w window) (*http.Response, error) {
	return c.api().GetProjects(ctx, &ytapi.GetProjectsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiGetProjectCustomFields(ctx context.Context, code, fields string, top int32) (*http.Response, error) {
	return c.api().GetProjectCustomFields(ctx, code, &ytapi.GetProjectCustomFieldsParams{Fields: &fields, Top: &top})
}

func (c *Client) apiGetProjectCustomField(ctx context.Context, code, id, fields string) (*http.Response, error) {
	return c.api().GetProjectCustomField(ctx, code, id, &ytapi.GetProjectCustomFieldParams{Fields: &fields})
}

func (c *Client) apiGetCustomFields(ctx context.Context, fields string, top int32) (*http.Response, error) {
	return c.api().GetCustomFields(ctx, &ytapi.GetCustomFieldsParams{Fields: &fields, Top: &top})
}

func (c *Client) apiGetIssue(ctx context.Context, id, fields string, named []string) (*http.Response, error) {
	params := ytapi.GetIssueParams{Fields: &fields}
	if len(named) > 0 {
		params.CustomFields = &named
	}
	return c.api().GetIssue(ctx, id, &params)
}

func (c *Client) apiCreateIssue(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueParams{Fields: &fields}
	return c.api().CreateIssueWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiUpdateIssue(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateIssueParams{Fields: &fields}
	return c.api().UpdateIssueWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiAddLinkedIssue(ctx context.Context, id, link string, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AddLinkedIssueParams{Fields: &fields}
	return c.api().AddLinkedIssueWithBody(ctx, id, link, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiRemoveLinkedIssue(ctx context.Context, id, link, targetInternalID string) (*http.Response, error) {
	return c.api().RemoveLinkedIssue(ctx, id, link, targetInternalID)
}

func (c *Client) apiDeleteIssue(ctx context.Context, at readableID) (*http.Response, error) {
	return c.api().DeleteIssue(ctx, at.readable)
}

func (c *Client) apiGetIssues(ctx context.Context, query, fields string, named []string, w window) (*http.Response, error) {
	params := ytapi.GetIssuesParams{Query: &query, Fields: &fields, Top: &w.top, Skip: w.skipped()}
	if len(named) > 0 {
		params.CustomFields = &named
	}
	return c.api().GetIssues(ctx, &params)
}

func (c *Client) apiCountIssues(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CountIssuesParams{Fields: &fields}
	return c.api().CountIssuesWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiAssistSearch(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AssistSearchParams{Fields: &fields}
	return c.api().AssistSearchWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiGetIssueActivities(ctx context.Context, id, categories, fields string, w window) (*http.Response, error) {
	newestFirst := true
	params := ytapi.GetIssueActivitiesParams{
		Categories: &categories,
		Reverse:    &newestFirst,
		Fields:     &fields,
		Top:        &w.top,
		Skip:       w.skipped(),
	}
	return c.api().GetIssueActivities(ctx, id, &params)
}

func (c *Client) apiGetIssueLinkTypes(ctx context.Context, fields string, top int32) (*http.Response, error) {
	return c.api().GetIssueLinkTypes(ctx, &ytapi.GetIssueLinkTypesParams{Fields: &fields, Top: &top})
}

func (c *Client) apiGetArticle(ctx context.Context, id, fields string) (*http.Response, error) {
	return c.api().GetArticle(ctx, id, &ytapi.GetArticleParams{Fields: &fields})
}

func (c *Client) apiGetArticles(ctx context.Context, query, fields string, w window) (*http.Response, error) {
	return c.api().GetArticles(ctx, &ytapi.GetArticlesParams{Fields: &fields, Top: &w.top, Skip: w.skipped(), Query: &query})
}

func (c *Client) apiGetArticleChildArticles(ctx context.Context, parent, fields string, w window) (*http.Response, error) {
	params := ytapi.GetArticleChildArticlesParams{Fields: &fields, Top: &w.top, Skip: w.skipped()}
	return c.api().GetArticleChildArticles(ctx, parent, &params)
}

func (c *Client) apiCreateArticle(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateArticleParams{Fields: &fields}
	return c.api().CreateArticleWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiUpdateArticle(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateArticleParams{Fields: &fields}
	return c.api().UpdateArticleWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiDeleteArticle(ctx context.Context, at readableID) (*http.Response, error) {
	return c.api().DeleteArticle(ctx, at.readable)
}

func (c *Client) apiCreateIssueComment(ctx context.Context, at owner, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueCommentParams{Fields: &fields}
	return c.api().CreateIssueCommentWithBody(ctx, at.id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiCreateArticleComment(ctx context.Context, at owner, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateArticleCommentParams{Fields: &fields}
	return c.api().CreateArticleCommentWithBody(ctx, at.id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiGetIssueComments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.api().GetIssueComments(ctx, at.id, &ytapi.GetIssueCommentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiGetArticleComments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.api().GetArticleComments(ctx, at.id, &ytapi.GetArticleCommentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiGetIssueComment(ctx context.Context, at owner, comment childID, fields string) (*http.Response, error) {
	params := ytapi.GetIssueCommentParams{Fields: &fields}
	return c.api().GetIssueComment(ctx, at.id, comment.id, &params)
}

func (c *Client) apiUpdateIssueComment(ctx context.Context, at owner, comment childID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateIssueCommentParams{Fields: &fields}
	return c.api().UpdateIssueCommentWithBody(ctx, at.id, comment.id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiUpdateArticleComment(ctx context.Context, at owner, comment childID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateArticleCommentParams{Fields: &fields}
	return c.api().UpdateArticleCommentWithBody(ctx, at.id, comment.id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiDeleteIssueComment(ctx context.Context, at owner, comment childID) (*http.Response, error) {
	return c.api().DeleteIssueComment(ctx, at.id, comment.id)
}

func (c *Client) apiDeleteArticleComment(ctx context.Context, at owner, comment childID) (*http.Response, error) {
	return c.api().DeleteArticleComment(ctx, at.id, comment.id)
}

func (c *Client) apiGetIssueWorkItems(ctx context.Context, id, fields string, w window) (*http.Response, error) {
	return c.api().GetIssueWorkItems(ctx, id, &ytapi.GetIssueWorkItemsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiCreateIssueWorkItem(ctx context.Context, id string, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueWorkItemParams{Fields: &fields}
	return c.api().CreateIssueWorkItemWithBody(ctx, id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiUpdateIssueWorkItem(ctx context.Context, id string, item childID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateIssueWorkItemParams{Fields: &fields}
	return c.api().UpdateIssueWorkItemWithBody(ctx, id, item.id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiGetIssueWorkItem(ctx context.Context, id string, item childID, fields string) (*http.Response, error) {
	params := ytapi.GetIssueWorkItemParams{Fields: &fields}
	return c.api().GetIssueWorkItem(ctx, id, item.id, &params)
}

func (c *Client) apiDeleteIssueWorkItem(ctx context.Context, at readableID, item childID) (*http.Response, error) {
	return c.api().DeleteIssueWorkItem(ctx, at.readable, item.id)
}

func (c *Client) apiGetIssueAttachments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.api().GetIssueAttachments(ctx, at.id, &ytapi.GetIssueAttachmentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiGetArticleAttachments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.api().GetArticleAttachments(ctx, at.id, &ytapi.GetArticleAttachmentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiCreateIssueAttachment(ctx context.Context, at owner, multipartType string, body io.Reader, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueAttachmentParams{Fields: &fields}
	return c.api().CreateIssueAttachmentWithBody(ctx, at.id, &params, multipartType, body)
}

func (c *Client) apiCreateArticleAttachment(ctx context.Context, at owner, multipartType string, body io.Reader, fields string) (*http.Response, error) {
	params := ytapi.CreateArticleAttachmentParams{Fields: &fields}
	return c.api().CreateArticleAttachmentWithBody(ctx, at.id, &params, multipartType, body)
}

func (c *Client) apiGetIssueAttachment(ctx context.Context, at owner, file childID, fields string) (*http.Response, error) {
	return c.api().GetIssueAttachment(ctx, at.id, file.id, &ytapi.GetIssueAttachmentParams{Fields: &fields})
}

func (c *Client) apiGetArticleAttachment(ctx context.Context, at owner, file childID, fields string) (*http.Response, error) {
	return c.api().GetArticleAttachment(ctx, at.id, file.id, &ytapi.GetArticleAttachmentParams{Fields: &fields})
}

func (c *Client) apiDeleteIssueAttachment(ctx context.Context, at readableID, file childID) (*http.Response, error) {
	return c.api().DeleteIssueAttachment(ctx, at.readable, file.id)
}

func (c *Client) apiDeleteArticleAttachment(ctx context.Context, at readableID, file childID) (*http.Response, error) {
	return c.api().DeleteArticleAttachment(ctx, at.readable, file.id)
}

func (c *Client) apiGetTags(ctx context.Context, fields string, w window) (*http.Response, error) {
	return c.api().GetTags(ctx, &ytapi.GetTagsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiCreateTag(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateTagParams{Fields: &fields}
	return c.api().CreateTagWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiDeleteTag(ctx context.Context, tag tagID) (*http.Response, error) {
	return c.api().DeleteTag(ctx, tag.id)
}

func (c *Client) apiAddIssueTag(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AddIssueTagParams{Fields: &fields}
	return c.api().AddIssueTagWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiAddArticleTag(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AddArticleTagParams{Fields: &fields}
	return c.api().AddArticleTagWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiRemoveIssueTag(ctx context.Context, at readableID, tag tagID) (*http.Response, error) {
	return c.api().RemoveIssueTag(ctx, at.readable, tag.id)
}

func (c *Client) apiRemoveArticleTag(ctx context.Context, at readableID, tag tagID) (*http.Response, error) {
	return c.api().RemoveArticleTag(ctx, at.readable, tag.id)
}

func (c *Client) apiGetGroups(ctx context.Context, fields string, top int32) (*http.Response, error) {
	return c.api().GetGroups(ctx, &ytapi.GetGroupsParams{Fields: &fields, Top: &top})
}

func (c *Client) apiGetUser(ctx context.Context, login, fields string) (*http.Response, error) {
	return c.api().GetUser(ctx, login, &ytapi.GetUserParams{Fields: &fields})
}

func (c *Client) apiGetUsers(ctx context.Context, search, fields string, w window) (*http.Response, error) {
	return c.api().GetUsers(ctx, &ytapi.GetUsersParams{Fields: &fields, Top: &w.top, Skip: w.skipped(), Query: &search})
}

func (c *Client) apiGetCurrentUser(ctx context.Context, fields string) (*http.Response, error) {
	return c.api().GetCurrentUser(ctx, &ytapi.GetCurrentUserParams{Fields: &fields})
}
