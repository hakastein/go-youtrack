package youtrack

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

const AttachmentListFields = "id,name,size,mimeType,url"

const attachmentsPlural = "attachments"

const (
	sizeKey       = "size"
	attachmentKey = "attachment"
	filePart      = "files[0]"
)

type attachmentTarget struct {
	schema string
	owner  string
	kind   ownerKind
}

func issueAttachmentTarget() attachmentTarget {
	return attachmentTarget{schema: issueAttachmentSchema, owner: "issue", kind: issueOwner}
}

func articleAttachmentTarget() attachmentTarget {
	return attachmentTarget{schema: articleAttachmentSchema, owner: "article", kind: articleOwner}
}

func attachmentTargetOf(kind ownerKind) attachmentTarget {
	if kind == articleOwner {
		return articleAttachmentTarget()
	}
	return issueAttachmentTarget()
}

func (h attachmentTarget) listSchema() string {
	return "[]" + h.schema
}

func ListAttachments(id string, expression string, page Page) (Call, *Error) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	page, fault = page.parse()
	if fault != nil {
		return nil, fault
	}
	requested, fault := attachmentFields(expression)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.listAttachments(ctx, spec, at, requested, page)
	}, nil
}

func (c *Client) listAttachments(ctx context.Context, spec *schemas, at owner, requested []requestedField, page Page) (*Node, *Error) {
	return c.listPage(ctx, spec, attachmentsPlural, attachmentTargetOf(at.kind).listSchema(), requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.getAttachments(ctx, at, fields, w)
	})
}

func (c *Client) getAttachments(ctx context.Context, at owner, fields string, w window) (*http.Response, error) {
	if at.kind == articleOwner {
		return c.apiGetArticleAttachments(ctx, at, fields, w)
	}
	return c.apiGetIssueAttachments(ctx, at, fields, w)
}

func attachmentFields(expression string) ([]requestedField, *Error) {
	if expression == "" {
		return parseDefault(AttachmentListFields, false)
	}
	return parseFields(expression, AttachmentListFields)
}

type AttachedFile struct {
	Name string
	Body io.ReadCloser
}

type FileOpener func() (AttachedFile, *Error)

func CreateAttachment(id string, open FileOpener, expression string) (Call, *Error) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	file, fault := open()
	if fault != nil {
		return nil, fault
	}
	sent, fault := newUpload(file)
	if fault != nil {
		return nil, fault
	}
	requested, fault := attachmentFields(expression)
	if fault != nil {
		_ = sent.body.Close()
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.createAttachment(ctx, spec, at, sent, requested)
	}, nil
}

func (c *Client) createAttachment(ctx context.Context, spec *schemas, at owner, sent *upload, requested []requestedField) (*Node, *Error) {
	body, contentType, confirmed := sent.form()
	return c.write(ctx, spec, attachmentTargetOf(at.kind).listSchema(), withFields(requested, sent.verifyFields()...), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCreateAttachment(ctx, at, contentType, body, fields)
	}, confirmed, writeResultNode(requested))
}

func (c *Client) apiCreateAttachment(ctx context.Context, at owner, contentType string, body io.Reader, fields string) (*http.Response, error) {
	if at.kind == articleOwner {
		return c.apiCreateArticleAttachment(ctx, at, contentType, body, fields)
	}
	return c.apiCreateIssueAttachment(ctx, at, contentType, body, fields)
}

func DeleteAttachment(id, attachment string) (Call, *Error) {
	at, fault := parseOwner(id)
	if fault != nil {
		return nil, fault
	}
	file, fault := parseChildID(attachmentKey, "the issue or the article", attachment)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*Node, *Error) {
		return c.deleteAttachment(ctx, spec, at, file)
	}, nil
}

func (c *Client) deleteAttachment(ctx context.Context, spec *schemas, at owner, file childID) (*Node, *Error) {
	target := attachmentTargetOf(at.kind)
	requested := []requestedField{
		{name: idKey},
		{name: nameKey},
		{name: target.owner, children: []requestedField{{name: idReadableKey}}},
	}
	found, fault := c.request(ctx, spec, target.schema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getAttachment(ctx, at, file, fields)
	})
	if fault != nil {
		return nil, fault
	}
	returnedOwner, fault := attachmentOwner(found, target, file)
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.apiDeleteAttachment(ctx, at.kind, returnedOwner, file)
	}); fault != nil {
		return nil, fault
	}
	return objectNode(found, requested, found.objects[0], nil)
}

func attachmentOwner(a decodedResponse, target attachmentTarget, file childID) (readableID, *Error) {
	received, isText := a.objects[0][idKey].(string)
	if !isText || received != file.id {
		message := fmt.Sprintf("the %s asked for under id %s arrived under another id", attachmentKey, quote(file.String()))
		return readableID{}, shapeFailure(a.httpResponse, a.body, message)
	}
	holder, isObject := a.objects[0][target.owner].(map[string]any)
	if !isObject {
		message := fmt.Sprintf("the %s the %s hangs from arrived as something other than an object",
			target.kind, attachmentKey)
		return readableID{}, shapeFailure(a.httpResponse, a.body, message)
	}
	return readableIDAt(a, holder, target.kind, "a deletion")
}

func (c *Client) getAttachment(ctx context.Context, at owner, file childID, fields string) (*http.Response, error) {
	if at.kind == articleOwner {
		return c.apiGetArticleAttachment(ctx, at, file, fields)
	}
	return c.apiGetIssueAttachment(ctx, at, file, fields)
}

func (c *Client) apiDeleteAttachment(ctx context.Context, kind ownerKind, at readableID, file childID) (*http.Response, error) {
	if kind == articleOwner {
		return c.apiDeleteArticleAttachment(ctx, at, file)
	}
	return c.apiDeleteIssueAttachment(ctx, at, file)
}

type upload struct {
	name string
	body io.ReadCloser
}

func newUpload(file AttachedFile) (*upload, *Error) {
	if fault := checkFileName(file.Name); fault != nil {
		_ = file.Body.Close()
		return nil, fault
	}
	return &upload{name: file.Name, body: file.Body}, nil
}

func (u *upload) form() (io.ReadCloser, string, func(decodedResponse) *Error) {
	reading, writing := io.Pipe()
	form := multipart.NewWriter(writing)
	var sent atomic.Int64
	go func() {
		defer u.body.Close()
		part, err := form.CreateFormFile(filePart, u.name)
		if err != nil {
			_ = writing.CloseWithError(err)
			return
		}
		written, err := io.Copy(part, u.body)
		sent.Store(written)
		if err != nil {
			_ = writing.CloseWithError(err)
			return
		}
		_ = writing.CloseWithError(form.Close())
	}()
	confirmed := func(a decodedResponse) *Error {
		return u.verify(a, sent.Load())
	}
	return reading, form.FormDataContentType(), confirmed
}

func (u *upload) verifyFields() []requestedField {
	return []requestedField{{name: nameKey}, {name: sizeKey}}
}

func (u *upload) verify(a decodedResponse, sent int64) *Error {
	if len(a.objects) != 1 {
		return ambiguousAttachmentFault(a)
	}
	filed := a.objects[0]
	wrong := textMismatch(nil, nameKey, u.name, filed[nameKey])
	wrong = sizeMismatch(wrong, sent, filed[sizeKey])
	if len(wrong) == 0 {
		return nil
	}
	return mismatchFault(a, knownAs(attachmentKey, responseID(a, idKey)), wrong)
}

func ambiguousAttachmentFault(a decodedResponse) *Error {
	details := []Pair{
		requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted()),
		{Key: "actual_count", Value: intNode(len(a.objects))},
		bodyDetail(a.body),
	}
	message := "one file was sent and the answer carries something other than the one attachment it was filed as"
	return &Error{Code: CodeUpstreamInvalid, Message: message, Details: details}
}

const maxTrimmedRune = ' '

func checkFileName(name string) *Error {
	because, rewritten := rewrittenName(name)
	if !rewritten {
		return nil
	}
	message := fmt.Sprintf("the file would be attached as %s, and %s. Attach a copy of it made under another name",
		quote(name), because)
	return &Error{Code: CodeBadUsage, Message: message}
}

func rewrittenName(name string) (because string, rewritten bool) {
	switch {
	case !utf8.ValidString(name):
		return "the bytes of that name are no valid UTF-8: YouTrack would keep each byte it cannot read as U+FFFD", true
	case strings.Contains(name, `\`):
		return `it holds a backslash, and YouTrack keeps only what stands after the last one`, true
	case strings.Contains(name, `"`):
		return `it holds a double quote, which the form carries escaped with a backslash, and YouTrack keeps only what stands after that backslash`, true
	case strings.ContainsAny(name, "\r\n"):
		return "it holds a carriage return or a line feed, which the form carries as %0D or %0A, and YouTrack keeps those four characters", true
	}
	first, _ := utf8.DecodeRuneInString(name)
	last, _ := utf8.DecodeLastRuneInString(name)
	if first <= maxTrimmedRune || last <= maxTrimmedRune {
		return fmt.Sprintf("it begins or ends with a character no greater than U+%04X, which YouTrack trims away", maxTrimmedRune), true
	}
	return "", false
}
