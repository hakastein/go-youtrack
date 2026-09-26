package youtrack

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/hakastein/go-youtrack/ytapi"
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

func (c *Client) apiGetProject(ctx context.Context, code, fields string) (*http.Response, error) {
	return c.API().GetProject(ctx, code, &ytapi.GetProjectParams{Fields: &fields})
}

func (c *Client) apiGetProjects(ctx context.Context, fields string, w window) (*http.Response, error) {
	return c.API().GetProjects(ctx, &ytapi.GetProjectsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiGetProjectCustomFields(ctx context.Context, code, fields string, top int32) (*http.Response, error) {
	return c.API().GetProjectCustomFields(ctx, code, &ytapi.GetProjectCustomFieldsParams{Fields: &fields, Top: &top})
}

func (c *Client) apiGetProjectCustomField(ctx context.Context, code, id, fields string) (*http.Response, error) {
	return c.API().GetProjectCustomField(ctx, code, id, &ytapi.GetProjectCustomFieldParams{Fields: &fields})
}

func (c *Client) apiGetCustomFields(ctx context.Context, fields string, top int32) (*http.Response, error) {
	return c.API().GetCustomFields(ctx, &ytapi.GetCustomFieldsParams{Fields: &fields, Top: &top})
}

func (c *Client) apiGetIssue(ctx context.Context, id, fields string, named []string) (*http.Response, error) {
	params := ytapi.GetIssueParams{Fields: &fields}
	if len(named) > 0 {
		params.CustomFields = &named
	}
	return c.API().GetIssue(ctx, id, &params)
}

func (c *Client) apiCreateIssue(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueParams{Fields: &fields}
	return c.API().CreateIssueWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiUpdateIssue(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateIssueParams{Fields: &fields}
	return c.API().UpdateIssueWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiAddLinkedIssue(ctx context.Context, id, link string, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AddLinkedIssueParams{Fields: &fields}
	return c.API().AddLinkedIssueWithBody(ctx, id, link, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiRemoveLinkedIssue(ctx context.Context, id, link, targetInternalID string) (*http.Response, error) {
	return c.API().RemoveLinkedIssue(ctx, id, link, targetInternalID)
}

func (c *Client) apiDeleteIssue(ctx context.Context, at readableID) (*http.Response, error) {
	return c.API().DeleteIssue(ctx, at.readable)
}

func (c *Client) apiGetIssues(ctx context.Context, query, fields string, named []string, w window) (*http.Response, error) {
	params := ytapi.GetIssuesParams{Query: &query, Fields: &fields, Top: &w.top, Skip: w.skipped()}
	if len(named) > 0 {
		params.CustomFields = &named
	}
	return c.API().GetIssues(ctx, &params)
}

func (c *Client) apiCountIssues(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CountIssuesParams{Fields: &fields}
	return c.API().CountIssuesWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiAssistSearch(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AssistSearchParams{Fields: &fields}
	return c.API().AssistSearchWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
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
	return c.API().GetIssueActivities(ctx, id, &params)
}

func (c *Client) apiGetIssueLinkTypes(ctx context.Context, fields string, top int32) (*http.Response, error) {
	return c.API().GetIssueLinkTypes(ctx, &ytapi.GetIssueLinkTypesParams{Fields: &fields, Top: &top})
}

func (c *Client) apiGetArticle(ctx context.Context, id, fields string) (*http.Response, error) {
	return c.API().GetArticle(ctx, id, &ytapi.GetArticleParams{Fields: &fields})
}

func (c *Client) apiGetArticles(ctx context.Context, query, fields string, w window) (*http.Response, error) {
	return c.API().GetArticles(ctx, &ytapi.GetArticlesParams{Fields: &fields, Top: &w.top, Skip: w.skipped(), Query: &query})
}

func (c *Client) apiGetArticleChildArticles(ctx context.Context, parent, fields string, w window) (*http.Response, error) {
	params := ytapi.GetArticleChildArticlesParams{Fields: &fields, Top: &w.top, Skip: w.skipped()}
	return c.API().GetArticleChildArticles(ctx, parent, &params)
}

func (c *Client) apiCreateArticle(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateArticleParams{Fields: &fields}
	return c.API().CreateArticleWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiUpdateArticle(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateArticleParams{Fields: &fields}
	return c.API().UpdateArticleWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiDeleteArticle(ctx context.Context, at readableID) (*http.Response, error) {
	return c.API().DeleteArticle(ctx, at.readable)
}

func (c *Client) apiCreateIssueComment(ctx context.Context, at owner, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueCommentParams{Fields: &fields}
	return c.API().CreateIssueCommentWithBody(ctx, at.id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiCreateArticleComment(ctx context.Context, at owner, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateArticleCommentParams{Fields: &fields}
	return c.API().CreateArticleCommentWithBody(ctx, at.id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiGetIssueComments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.API().GetIssueComments(ctx, at.id, &ytapi.GetIssueCommentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiGetArticleComments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.API().GetArticleComments(ctx, at.id, &ytapi.GetArticleCommentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiGetIssueComment(ctx context.Context, at owner, comment childID, fields string) (*http.Response, error) {
	params := ytapi.GetIssueCommentParams{Fields: &fields}
	return c.API().GetIssueComment(ctx, at.id, comment.id, &params)
}

func (c *Client) apiUpdateIssueComment(ctx context.Context, at owner, comment childID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateIssueCommentParams{Fields: &fields}
	return c.API().UpdateIssueCommentWithBody(ctx, at.id, comment.id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiUpdateArticleComment(ctx context.Context, at owner, comment childID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateArticleCommentParams{Fields: &fields}
	return c.API().UpdateArticleCommentWithBody(ctx, at.id, comment.id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiDeleteIssueComment(ctx context.Context, at owner, comment childID) (*http.Response, error) {
	return c.API().DeleteIssueComment(ctx, at.id, comment.id)
}

func (c *Client) apiDeleteArticleComment(ctx context.Context, at owner, comment childID) (*http.Response, error) {
	return c.API().DeleteArticleComment(ctx, at.id, comment.id)
}

func (c *Client) apiGetIssueWorkItems(ctx context.Context, id, fields string, w window) (*http.Response, error) {
	return c.API().GetIssueWorkItems(ctx, id, &ytapi.GetIssueWorkItemsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiCreateIssueWorkItem(ctx context.Context, id string, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueWorkItemParams{Fields: &fields}
	return c.API().CreateIssueWorkItemWithBody(ctx, id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiUpdateIssueWorkItem(ctx context.Context, id string, item childID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.UpdateIssueWorkItemParams{Fields: &fields}
	return c.API().UpdateIssueWorkItemWithBody(ctx, id, item.id, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiGetIssueWorkItem(ctx context.Context, id string, item childID, fields string) (*http.Response, error) {
	params := ytapi.GetIssueWorkItemParams{Fields: &fields}
	return c.API().GetIssueWorkItem(ctx, id, item.id, &params)
}

func (c *Client) apiDeleteIssueWorkItem(ctx context.Context, at readableID, item childID) (*http.Response, error) {
	return c.API().DeleteIssueWorkItem(ctx, at.readable, item.id)
}

func (c *Client) apiGetIssueAttachments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.API().GetIssueAttachments(ctx, at.id, &ytapi.GetIssueAttachmentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiGetArticleAttachments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	return c.API().GetArticleAttachments(ctx, at.id, &ytapi.GetArticleAttachmentsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiCreateIssueAttachment(ctx context.Context, at owner, multipartType string, body io.Reader, fields string) (*http.Response, error) {
	params := ytapi.CreateIssueAttachmentParams{Fields: &fields}
	return c.API().CreateIssueAttachmentWithBody(ctx, at.id, &params, multipartType, body)
}

func (c *Client) apiCreateArticleAttachment(ctx context.Context, at owner, multipartType string, body io.Reader, fields string) (*http.Response, error) {
	params := ytapi.CreateArticleAttachmentParams{Fields: &fields}
	return c.API().CreateArticleAttachmentWithBody(ctx, at.id, &params, multipartType, body)
}

func (c *Client) apiGetIssueAttachment(ctx context.Context, at owner, file childID, fields string) (*http.Response, error) {
	return c.API().GetIssueAttachment(ctx, at.id, file.id, &ytapi.GetIssueAttachmentParams{Fields: &fields})
}

func (c *Client) apiGetArticleAttachment(ctx context.Context, at owner, file childID, fields string) (*http.Response, error) {
	return c.API().GetArticleAttachment(ctx, at.id, file.id, &ytapi.GetArticleAttachmentParams{Fields: &fields})
}

func (c *Client) apiDeleteIssueAttachment(ctx context.Context, at readableID, file childID) (*http.Response, error) {
	return c.API().DeleteIssueAttachment(ctx, at.readable, file.id)
}

func (c *Client) apiDeleteArticleAttachment(ctx context.Context, at readableID, file childID) (*http.Response, error) {
	return c.API().DeleteArticleAttachment(ctx, at.readable, file.id)
}

func (c *Client) apiGetTags(ctx context.Context, fields string, w window) (*http.Response, error) {
	return c.API().GetTags(ctx, &ytapi.GetTagsParams{Fields: &fields, Top: &w.top, Skip: w.skipped()})
}

func (c *Client) apiCreateTag(ctx context.Context, body []byte, fields string) (*http.Response, error) {
	params := ytapi.CreateTagParams{Fields: &fields}
	return c.API().CreateTagWithBody(ctx, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiDeleteTag(ctx context.Context, tag tagID) (*http.Response, error) {
	return c.API().DeleteTag(ctx, tag.id)
}

func (c *Client) apiAddIssueTag(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AddIssueTagParams{Fields: &fields}
	return c.API().AddIssueTagWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiAddArticleTag(ctx context.Context, at readableID, body []byte, fields string) (*http.Response, error) {
	params := ytapi.AddArticleTagParams{Fields: &fields}
	return c.API().AddArticleTagWithBody(ctx, at.readable, &params, jsonContentType, bytes.NewReader(body))
}

func (c *Client) apiRemoveIssueTag(ctx context.Context, at readableID, tag tagID) (*http.Response, error) {
	return c.API().RemoveIssueTag(ctx, at.readable, tag.id)
}

func (c *Client) apiRemoveArticleTag(ctx context.Context, at readableID, tag tagID) (*http.Response, error) {
	return c.API().RemoveArticleTag(ctx, at.readable, tag.id)
}

func (c *Client) apiGetGroups(ctx context.Context, fields string, top int32) (*http.Response, error) {
	return c.API().GetGroups(ctx, &ytapi.GetGroupsParams{Fields: &fields, Top: &top})
}

func (c *Client) apiGetUser(ctx context.Context, login, fields string) (*http.Response, error) {
	return c.API().GetUser(ctx, login, &ytapi.GetUserParams{Fields: &fields})
}

func (c *Client) apiGetUsers(ctx context.Context, search, fields string, w window) (*http.Response, error) {
	return c.API().GetUsers(ctx, &ytapi.GetUsersParams{Fields: &fields, Top: &w.top, Skip: w.skipped(), Query: &search})
}

func (c *Client) apiGetCurrentUser(ctx context.Context, fields string) (*http.Response, error) {
	return c.API().GetCurrentUser(ctx, &ytapi.GetCurrentUserParams{Fields: &fields})
}
