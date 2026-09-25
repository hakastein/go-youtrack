package youtrack

import (
	"context"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"
)

func newSendOnceHTTPClient() *http.Client {
	var http1Only http.Protocols
	http1Only.SetHTTP1(true)
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			// net/http silently resends a request over HTTP/2 on GOAWAY and over a reused connection that broke.
			Protocols:         &http1Only,
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (c *Client) authorize(_ context.Context, request *http.Request) error {
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	return nil
}

// Send sends a write through call and tells a request that never left from one the server may have acted
// on: a *TransportError with Written set is a write whose outcome is unknown.
func Send(ctx context.Context, call func(ctx context.Context) (*http.Response, error)) (*http.Response, error) {
	// net/http tells an unsent request only by an unexported error, and a server does not act on a partial one.
	var requestWritten atomic.Bool
	traced := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(wrote httptrace.WroteRequestInfo) {
			if wrote.Err == nil {
				requestWritten.Store(true)
			}
		},
	})
	response, err := call(traced)
	if err != nil {
		return nil, transportError(err, requestWritten.Load())
	}
	return response, nil
}
