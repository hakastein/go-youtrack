package youtrack

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

// Request is a request the way the caller may send it again: the method and the URL with a readable query.
type Request struct {
	Method string
	URL    string
}

func (r Request) String() string {
	return r.Method + " " + r.URL
}

func requestOf(response *http.Response) Request {
	return Request{Method: response.Request.Method, URL: readableURL(response.Request.URL.Redacted())}
}

func readableURL(address string) string {
	path, query, split := strings.Cut(address, "?")
	if !split {
		return address
	}
	return path + "?" + readableQuery(query)
}

func readableQuery(query string) string {
	return strings.NewReplacer("%24", "$", "%28", "(", "%29", ")", "%2C", ",").Replace(query)
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
