package youtrack

import (
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	Issues      *IssuesService
	Articles    *ArticlesService
	Comments    *CommentsService
	Attachments *AttachmentsService
	Links       *LinksService
	Tags        *TagsService
	WorkItems   *WorkItemsService
	Activities  *ActivitiesService
	Projects    *ProjectsService
	Fields      *FieldsService
	Users       *UsersService

	address    *url.URL
	token      string
	httpClient *http.Client
	cache      metaCache
	spec       *schemas
}

type service struct {
	client *Client
}

type IssuesService service

type ArticlesService service

type CommentsService service

type AttachmentsService service

type LinksService service

type TagsService service

type WorkItemsService service

type ActivitiesService service

type ProjectsService service

type FieldsService service

type UsersService service

type Option func(*Client)

// WithMetadataCache keeps the metadata of projects and the custom fields of the instance: a directory 0700 per
// address and token, files 0600, no token inside; an empty dir keeps none.
func WithMetadataCache(dir string) Option {
	return func(c *Client) {
		c.cache = newMetaCache(dir, c.address.String(), c.token)
	}
}

// NewClient is a client of the instance at address, which ParseAddress takes, with a permanent token, which
// CheckToken takes.
func NewClient(address, token string, opts ...Option) (*Client, error) {
	parsed, err := ParseAddress(address)
	if err != nil {
		return nil, err
	}
	if err := CheckToken(token); err != nil {
		return nil, err
	}
	c := &Client{address: parsed, token: token, httpClient: newSendOnceHTTPClient(), spec: loadSchemas()}
	for _, opt := range opts {
		opt(c)
	}
	common := service{client: c}
	c.Issues = (*IssuesService)(&common)
	c.Articles = (*ArticlesService)(&common)
	c.Comments = (*CommentsService)(&common)
	c.Attachments = (*AttachmentsService)(&common)
	c.Links = (*LinksService)(&common)
	c.Tags = (*TagsService)(&common)
	c.WorkItems = (*WorkItemsService)(&common)
	c.Activities = (*ActivitiesService)(&common)
	c.Projects = (*ProjectsService)(&common)
	c.Fields = (*FieldsService)(&common)
	c.Users = (*UsersService)(&common)
	return c, nil
}

// ParseAddress takes an absolute http or https URL with a host and an optional path, and nothing else: no user, no
// password, no query, no fragment. The refusal does not repeat the address, so nothing written in it is printed.
func ParseAddress(address string) (*url.URL, error) {
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || strings.ContainsAny(address, "?#") {
		return nil, &Error{Code: CodeBadUsage, Message: "the address is not a link like https://example.com"}
	}
	return parsed, nil
}

// CheckToken takes a token a request header can carry: not empty, no control character but TAB.
func CheckToken(token string) error {
	if token == "" {
		return &Error{Code: CodeBadUsage, Message: "the token is empty"}
	}
	for i := range len(token) {
		if c := token[i]; c < ' ' && c != '\t' || c == 0x7f {
			return &Error{Code: CodeBadUsage, Message: "the token holds a control character, which no header carries"}
		}
	}
	return nil
}
