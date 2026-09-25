package youtrack

import (
	"net/http"
	"net/url"
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

// WithMetadataCache is for a process of one call, as a CLI: a directory 0700 per address and token, files 0600,
// no token inside.
func WithMetadataCache(dir string) Option {
	return func(c *Client) {
		c.cache = newMetaCache(dir, c.address.String(), c.token)
	}
}

// NewClient is a client of the instance at address, an absolute http or https URL, with a permanent token.
func NewClient(address, token string, opts ...Option) (*Client, error) {
	parsed, fault := parseAddress(address)
	if fault != nil {
		return nil, fault
	}
	if fault := checkToken(token); fault != nil {
		return nil, fault
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

func parseAddress(address string) (*url.URL, *Error) {
	parsed, err := url.Parse(address)
	var reason string
	switch {
	case err != nil:
		reason = "is no URL: " + err.Error()
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		reason = "is not an http or https URL"
	case parsed.Host == "":
		reason = "names no host"
	case parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "":
		reason = "carries a query or a fragment, and the address of an instance is a host and a path"
	default:
		return parsed, nil
	}
	return nil, &Error{Code: CodeBadUsage, Message: "address " + quote(address) + " " + reason}
}

// The token itself never goes into the message: an error is printed.
func checkToken(token string) *Error {
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
