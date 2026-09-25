package youtrack

import (
	"bytes"
	"encoding/json"
	"net/http"
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
