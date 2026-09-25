package youtrack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// The url in it is a signed link: it fetches the file without a token for up to three days.
const AttachmentListFields = "id,name,size,mimeType,url"

// ListAttachmentsOptions: Fields is a fields= expression, empty for AttachmentListFields and +x for them and x.
type ListAttachmentsOptions struct {
	Fields string
	Page   Page
}

type File struct {
	Name    string
	Content io.Reader
}

const attachmentsPlural = "attachments"

const (
	sizeKey       = "size"
	attachmentKey = "attachment"
	filePart      = "files[0]"
)

// The page holds the files of the comments of owner too.
func (s *AttachmentsService) List(ctx context.Context, owner string, opts *ListAttachmentsOptions) (*Node, error) {
	return result(s.list(ctx, owner, optionsOf(opts)))
}

// Create leaves Content for the caller to close; once it returns, only a Read under way when ctx was done may still
// touch it. A name YouTrack would keep as another is refused before the request.
func (s *AttachmentsService) Create(ctx context.Context, owner string, file File, opts *WriteOptions) (*Node, error) {
	return result(s.create(ctx, owner, file, optionsOf(opts)))
}

// Delete answers with the attachment as read just before the deletion: id is its internal id, as 12-1, and the
// deletion goes to the owner the read named.
func (s *AttachmentsService) Delete(ctx context.Context, owner, id string) (*Node, error) {
	return result(s.delete(ctx, owner, id))
}

type attachmentTarget struct {
	schema string
	list   func(c *Client, ctx context.Context, at owner, fields string, w window) (*http.Response, error)
	create func(c *Client, ctx context.Context, at owner, multipartType string, body io.Reader, fields string) (*http.Response, error)
	get    func(c *Client, ctx context.Context, at owner, file childID, fields string) (*http.Response, error)
	remove func(c *Client, ctx context.Context, at readableID, file childID) (*http.Response, error)
}

func attachmentTargetOf(kind ownerKind) attachmentTarget {
	if kind == articleOwner {
		return attachmentTarget{schema: articleAttachmentSchema, list: (*Client).apiGetArticleAttachments,
			create: (*Client).apiCreateArticleAttachment, get: (*Client).apiGetArticleAttachment,
			remove: (*Client).apiDeleteArticleAttachment}
	}
	return attachmentTarget{schema: issueAttachmentSchema, list: (*Client).apiGetIssueAttachments,
		create: (*Client).apiCreateIssueAttachment, get: (*Client).apiGetIssueAttachment,
		remove: (*Client).apiDeleteIssueAttachment}
}

func (s *AttachmentsService) list(ctx context.Context, owner string, opts ListAttachmentsOptions) (*Node, *Error) {
	c := s.client
	at, fault := parseOwner(owner)
	if fault != nil {
		return nil, fault
	}
	page, fault := opts.Page.parse()
	if fault != nil {
		return nil, fault
	}
	target := attachmentTargetOf(at.kind)
	requested, fault := c.parseFields(target.schema, opts.Fields, AttachmentListFields)
	if fault != nil {
		return nil, fault
	}
	return c.listPage(ctx, attachmentsPlural, "[]"+target.schema, requested, page, func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return target.list(c, ctx, at, fields, w)
	})
}

func (s *AttachmentsService) create(ctx context.Context, owner string, file File, opts WriteOptions) (*Node, *Error) {
	c := s.client
	at, fault := parseOwner(owner)
	if fault != nil {
		return nil, fault
	}
	if fault := checkFile(file); fault != nil {
		return nil, fault
	}
	target := attachmentTargetOf(at.kind)
	requested, fault := c.parseFields(target.schema, opts.Fields, AttachmentListFields)
	if fault != nil {
		return nil, fault
	}
	sent := newUpload(ctx, file.Content)
	defer sent.stop()
	body, contentType := sent.form(file.Name)
	confirmed := func(a decodedResponse) *Error {
		return verifyUpload(a, file.Name, sent.streamed())
	}
	checked := withFields(requested, requestedField{name: nameKey}, requestedField{name: sizeKey})
	return writeAs(ctx, c, "[]"+target.schema, checked, func(ctx context.Context, fields string) (*http.Response, error) {
		return target.create(c, ctx, at, contentType, body, fields)
	}, confirmed, writeResultNode(requested))
}

func (s *AttachmentsService) delete(ctx context.Context, owner, id string) (*Node, *Error) {
	c := s.client
	at, fault := parseOwner(owner)
	if fault != nil {
		return nil, fault
	}
	file, fault := parseChildID(attachmentKey, ownerNoun, id)
	if fault != nil {
		return nil, fault
	}
	target := attachmentTargetOf(at.kind)
	requested := []requestedField{
		{name: idKey},
		{name: nameKey},
		{name: at.kind.key(), children: []requestedField{{name: idReadableKey}}},
	}
	return c.deleteAsRead(ctx, target.schema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return target.get(c, ctx, at, file, fields)
	}, childOwner(attachmentKey, file, at.kind), func(ctx context.Context, holder readableID) (*http.Response, error) {
		return target.remove(c, ctx, holder, file)
	})
}

// net/http may read the body after the response, when the caller has already closed Content; a cancelled request
// returns only after that read, so Content is read apart and left behind when ctx is done.
type upload struct {
	ctx     context.Context
	content io.Reader
	reading chan struct{}
	stopped atomic.Bool
	read    atomic.Int64
}

func newUpload(ctx context.Context, content io.Reader) *upload {
	return &upload{ctx: ctx, content: content, reading: make(chan struct{}, 1)}
}

func (u *upload) Read(p []byte) (int, error) {
	select {
	case u.reading <- struct{}{}:
	case <-u.ctx.Done():
		return 0, u.ctx.Err()
	}
	if err := u.refusal(); err != nil {
		<-u.reading
		return 0, err
	}
	into := make([]byte, len(p))
	var err error
	done := make(chan int, 1)
	go func() {
		defer func() { <-u.reading }()
		var n int
		n, err = u.content.Read(into)
		done <- n
	}()
	select {
	case n := <-done:
		u.read.Add(int64(n))
		return copy(p, into[:n]), err
	case <-u.ctx.Done():
		return 0, u.ctx.Err()
	}
}

func (u *upload) refusal() error {
	if err := u.ctx.Err(); err != nil {
		return err
	}
	if u.stopped.Load() {
		return io.ErrClosedPipe
	}
	return nil
}

func (u *upload) stop() {
	u.stopped.Store(true)
	select {
	case u.reading <- struct{}{}:
		<-u.reading
	case <-u.ctx.Done():
	}
}

func (u *upload) streamed() int64 {
	return u.read.Load()
}

func (u *upload) form(name string) (io.Reader, string) {
	var framing bytes.Buffer
	form := multipart.NewWriter(&framing)
	// A multipart writer fails only where the writer under it does, and a bytes.Buffer takes every write.
	_, _ = form.CreateFormFile(filePart, name)
	head := bytes.Clone(framing.Bytes())
	framing.Reset()
	_ = form.Close()
	return io.MultiReader(bytes.NewReader(head), u, &framing), form.FormDataContentType()
}

func verifyUpload(a decodedResponse, name string, sent int64) *Error {
	if len(a.objects) != 1 {
		message := "one file was sent and the answer carries something other than the one attachment it was filed as"
		return a.fault(CodeUpstreamInvalid, message, Pair{Key: "actual_count", Value: intNode(len(a.objects))}, bodyDetail(a.body))
	}
	filed := a.objects[0]
	wrong := textMismatch(nil, nameKey, name, filed[nameKey])
	wrong = sizeMismatch(wrong, sent, filed[sizeKey])
	return mismatchFault(a, wrong, Pair{Key: attachmentKey, Value: responseID(a, idKey)})
}

func checkFile(file File) *Error {
	switch {
	case file.Name == "":
		return &Error{Code: CodeBadUsage, Message: "the file to attach has no name to be filed under"}
	case file.Content == nil:
		return &Error{Code: CodeBadUsage, Message: "the file to attach has nothing to read its bytes from, " +
			"and an empty file is one whose reader ends at once"}
	}
	return checkFileName(file.Name)
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
	if because := rewrittenReason("its name", name); because != "" {
		return because, true
	}
	switch {
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

func sizeMismatch(wrong []mismatch, bytesStreamed int64, value any) []mismatch {
	if received, isNumber := parseInt64(value); isNumber && received == bytesStreamed {
		return wrong
	}
	written := NewNumber(json.Number(strconv.FormatInt(bytesStreamed, 10)))
	return append(wrong, mismatch{field: sizeKey, expected: written, actual: rawValueNode(value)})
}
