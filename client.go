package youtrack

import (
	"net/http"
	"net/url"
)

// Client reaches one YouTrack instance with one permanent token.
type Client struct {
	address    *url.URL
	token      string
	httpClient *http.Client
	cache      metaCache
}

// Option configures a Client.
type Option func(*Client)

// WithMetadataCache keeps the field metadata of a project that Bundle reads under root, in a directory of
// the address and the token: the next Bundle of the same client skips the read of the project.
func WithMetadataCache(root string) Option {
	return func(c *Client) {
		c.cache = newMetaCache(root, c.address.String(), c.token)
	}
}

// New is a client of the instance at address, an absolute http or https URL, with a permanent token.
func New(address, token string, options ...Option) (*Client, error) {
	parsed, err := parseAddress(address)
	if err != nil {
		return nil, err
	}
	if err := checkToken(token); err != nil {
		return nil, err
	}
	c := &Client{address: parsed, token: token, httpClient: newSendOnceHTTPClient()}
	for _, option := range options {
		option(c)
	}
	return c, nil
}

func parseAddress(address string) (*url.URL, error) {
	parsed, err := url.Parse(address)
	switch {
	case err != nil:
		return nil, &ArgumentError{Argument: "address", Value: address, Reason: "is no URL: " + err.Error()}
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return nil, &ArgumentError{Argument: "address", Value: address, Reason: "is not an http or https URL"}
	case parsed.Host == "":
		return nil, &ArgumentError{Argument: "address", Value: address, Reason: "names no host"}
	case parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "":
		return nil, &ArgumentError{Argument: "address", Value: address, Reason: "carries a query or a fragment, and the address of an instance is a host and a path"}
	}
	return parsed, nil
}

func checkToken(token string) error {
	if token == "" {
		return &ArgumentError{Argument: "token", Value: token, Reason: "is empty"}
	}
	for i := range len(token) {
		if c := token[i]; c < ' ' && c != '\t' || c == 0x7f {
			return &ArgumentError{Argument: "token", Value: token, Reason: "holds a control character, which no header carries"}
		}
	}
	return nil
}
