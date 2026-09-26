package youtrack_test

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hakastein/go-youtrack"
	"github.com/hakastein/go-youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func attachmentFile(name, content string) youtrack.File {
	return youtrack.File{Name: name, Content: strings.NewReader(content)}
}

func attachmentFiled(schema, name, size string) string {
	return `{"$type":` + strconv.Quote(schema) + `,"id":"12-9","name":` + name + `,"size":` + size + `}`
}

func attachmentFiledOnce(name string, size int) string {
	return `[` + attachmentFiled("IssueAttachment", strconv.Quote(name), strconv.Itoa(size)) + `]`
}

type attachmentPart struct {
	field       string
	file        string
	disposition string
	content     string
}

func attachmentParts(t *testing.T, sent fake.Request) []attachmentPart {
	t.Helper()
	kind, params, err := mime.ParseMediaType(sent.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", kind)
	form := multipart.NewReader(strings.NewReader(sent.Body), params["boundary"])
	var parts []attachmentPart
	for {
		part, err := form.NextPart()
		if errors.Is(err, io.EOF) {
			return parts
		}
		require.NoError(t, err)
		content, err := io.ReadAll(part)
		require.NoError(t, err)
		parts = append(parts, attachmentPart{
			field:       part.FormName(),
			file:        part.FileName(),
			disposition: part.Header.Get("Content-Disposition"),
			content:     string(content),
		})
	}
}

type closeRecorder struct {
	io.Reader
	closed bool
}

func (r *closeRecorder) Close() error {
	r.closed = true
	return nil
}

func TestAttachmentsRefuseAnOwnerThatIsNoReadableID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func(ctx context.Context, attachments *youtrack.AttachmentsService) error
	}{
		{
			name: "a list",
			call: func(ctx context.Context, attachments *youtrack.AttachmentsService) error {
				_, err := attachments.List(ctx, "12-5", &youtrack.ListAttachmentsOptions{Fields: "id"})
				return err
			},
		},
		{
			name: "an upload",
			call: func(ctx context.Context, attachments *youtrack.AttachmentsService) error {
				_, err := attachments.Create(ctx, "12-5", attachmentFile("one.txt", "x"), answeredWith("id"))
				return err
			},
		},
		{
			name: "a deletion",
			call: func(ctx context.Context, attachments *youtrack.AttachmentsService) error {
				_, err := attachments.Delete(ctx, "12-5", "12-6")
				return err
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.call(t.Context(), client(t, fake.ServeNothing(t)).Attachments)
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCreateAttachmentRefusesAFileWithoutANameOrBytesToRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file youtrack.File
	}{
		{name: "no name", file: attachmentFile("", "x")},
		{name: "no reader", file: youtrack.File{Name: "one.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).Attachments.Create(t.Context(), "DEV-1", tc.file, answeredWith("id"))
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCreateAttachmentRefusesANameTheServerWouldKeepAsAnother(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
	}{
		{name: "a double quote", file: `a"b.txt`},
		{name: "a backslash", file: `a\b.txt`},
		{name: "a line feed", file: "a\nb.txt"},
		{name: "a carriage return", file: "a\rb.txt"},
		{name: "a leading space", file: " a.txt"},
		{name: "a trailing space", file: "a.txt "},
		{name: "a leading tab", file: "\ta.txt"},
		{name: "a trailing vertical tab", file: "a.txt\v"},
		{name: "a leading NUL", file: "\x00a.txt"},
		{name: "bytes that are no UTF-8", file: "\xff.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).Attachments.Create(t.Context(), "DEV-1", attachmentFile(tc.file, "x"), answeredWith("id"))
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func TestCreateAttachmentSendsTheNameAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
	}{
		{name: "a space", file: "a b.txt"},
		{name: "a semicolon and a per cent", file: "a;100%.txt"},
		{name: "a tab inside", file: "a\tb.txt"},
		{name: "a line separator inside", file: "a\u2028b.txt"},
		{name: "a non-breaking space first", file: "\u00a0a.txt"},
		{name: "Cyrillic", file: "заметка.txt"},
		{name: "254 bytes of Cyrillic", file: strings.Repeat("я", 127)},
		{name: "three dots", file: "..."},
		{name: "a lone dash", file: "-"},
		{name: "a leading dash", file: "-a.txt"},
		{name: "no extension", file: "README"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, attachmentFiledOnce(tc.file, 1)))

			_, err := client(t, server).Attachments.Create(t.Context(), "DEV-1", attachmentFile(tc.file, "x"),
				answeredWith("id"))

			require.NoError(t, err)
			assert.Equal(t, []attachmentPart{{
				field:       "files[0]",
				file:        tc.file,
				disposition: `form-data; name="files[0]"; filename="` + tc.file + `"`,
				content:     "x",
			}}, attachmentParts(t, server.Last(t)))
		})
	}
}

func TestCreateAttachmentStreamsTheFileAsOnePart(t *testing.T) {
	t.Parallel()
	content := make([]byte, 300*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}
	tests := []struct {
		name  string
		owner string
		path  string
		filed string
	}{
		{
			name:  "to an issue",
			owner: "DEV-1",
			path:  "/api/issues/DEV-1/attachments",
			filed: `[` + attachmentFiled("IssueAttachment", `"one.bin"`, strconv.Itoa(len(content))) + `]`,
		},
		{
			name:  "to an article",
			owner: "DEV-A-1",
			path:  "/api/articles/DEV-A-1/attachments",
			filed: `[` + attachmentFiled("ArticleAttachment", `"one.bin"`, strconv.Itoa(len(content))) + `]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.filed))

			_, err := client(t, server).Attachments.Create(t.Context(), tc.owner, attachmentFile("one.bin", string(content)),
				answeredWith("id"))

			require.NoError(t, err)
			sent := server.Last(t)
			assert.Equal(t, http.MethodPost, sent.Method)
			assert.Equal(t, []string{tc.path + "?fields=id,name,size"}, server.Targets(t))
			assert.Equal(t, []string{"chunked"}, sent.TransferEncoding)
			assert.EqualValues(t, -1, sent.ContentLength)
			assert.Equal(t, []attachmentPart{{
				field:       "files[0]",
				file:        "one.bin",
				disposition: `form-data; name="files[0]"; filename="one.bin"`,
				content:     string(content),
			}}, attachmentParts(t, sent))
		})
	}
}

func TestCreateAttachmentAsksForTheNameAndTheSizeItChecks(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, attachmentFiledOnce("one.txt", 1)))

	node, err := client(t, server).Attachments.Create(t.Context(), "DEV-1", attachmentFile("one.txt", "x"),
		answeredWith("id"))

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "id", Value: youtrack.NewString("12-9")}), node)
	assert.Equal(t, []string{"id,name,size"}, server.Fields())
}

func TestCreateAttachmentLeavesClosingTheFileToTheCaller(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, attachmentFiledOnce("one.txt", 1)))
	content := &closeRecorder{Reader: strings.NewReader("x")}

	_, err := client(t, server).Attachments.Create(t.Context(), "DEV-1", youtrack.File{Name: "one.txt", Content: content},
		answeredWith("id"))

	require.NoError(t, err)
	assert.False(t, content.closed)
}

type stallingFile struct {
	cancel  context.CancelFunc
	release chan struct{}
}

func (f stallingFile) Read([]byte) (int, error) {
	f.cancel()
	<-f.release
	return 0, io.EOF
}

func TestCreateAttachmentReturnsOnACancellationWhileAReadOfTheFileHangs(t *testing.T) {
	t.Parallel()
	server := fake.ServeUnread(t, func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
	})
	ctx, cancel := context.WithCancel(t.Context())
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	file := youtrack.File{Name: "one.txt", Content: stallingFile{cancel: cancel, release: release}}

	_, err := client(t, server).Attachments.Create(ctx, "DEV-1", file, answeredWith("id"))

	assert.ErrorIs(t, err, context.Canceled)
	sent := requestTo(http.MethodPost, server, "/api/issues/DEV-1/attachments?fields=id,name,size")
	assert.Equal(t, youtrack.Error{Code: youtrack.CodeUpstreamFailed, Details: []youtrack.Pair{sent}}, errorOf(t, err))
}

func TestCreateAttachmentRefusesAnAnswerThatIsNotTheFileThatWentOut(t *testing.T) {
	t.Parallel()
	one := attachmentFiled("IssueAttachment", `"one.txt"`, "6")
	mismatched := func(field string, expected, actual *youtrack.Node) []youtrack.Pair {
		return []youtrack.Pair{
			{Key: "attachment", Value: youtrack.NewString("12-9")},
			{Key: "mismatch", Value: youtrack.NewList(mismatch(field, expected, actual))},
		}
	}
	tests := []struct {
		name    string
		owner   string
		path    string
		body    string
		details []youtrack.Pair
	}{
		{
			name:  "no attachment at all",
			owner: "DEV-1",
			path:  "/api/issues/DEV-1/attachments",
			body:  `[]`,
			details: []youtrack.Pair{
				{Key: "actual_count", Value: number(0)},
				{Key: "upstream_body", Value: youtrack.NewString(`[]`)},
			},
		},
		{
			name:  "two attachments",
			owner: "DEV-1",
			path:  "/api/issues/DEV-1/attachments",
			body:  `[` + one + `,` + one + `]`,
			details: []youtrack.Pair{
				{Key: "actual_count", Value: number(2)},
				{Key: "upstream_body", Value: youtrack.NewString(`[` + one + `,` + one + `]`)},
			},
		},
		{
			name:    "a name the server kept as another",
			owner:   "DEV-1",
			path:    "/api/issues/DEV-1/attachments",
			body:    `[` + attachmentFiled("IssueAttachment", `"other.txt"`, "6") + `]`,
			details: mismatched("name", youtrack.NewString("one.txt"), youtrack.NewString("other.txt")),
		},
		{
			name:    "a name that arrived as a number",
			owner:   "DEV-1",
			path:    "/api/issues/DEV-1/attachments",
			body:    `[` + attachmentFiled("IssueAttachment", `7`, "6") + `]`,
			details: mismatched("name", youtrack.NewString("one.txt"), number(7)),
		},
		{
			name:    "a size that is not the count of bytes that went out",
			owner:   "DEV-1",
			path:    "/api/issues/DEV-1/attachments",
			body:    `[` + attachmentFiled("IssueAttachment", `"one.txt"`, "7") + `]`,
			details: mismatched("size", number(6), number(7)),
		},
		{
			name:    "a size that arrived as text",
			owner:   "DEV-1",
			path:    "/api/issues/DEV-1/attachments",
			body:    `[` + attachmentFiled("IssueAttachment", `"one.txt"`, `"6"`) + `]`,
			details: mismatched("size", number(6), youtrack.NewString("6")),
		},
		{
			name:  "the one object the specification declares for an article",
			owner: "DEV-A-1",
			path:  "/api/articles/DEV-A-1/attachments",
			body:  attachmentFiled("ArticleAttachment", `"one.txt"`, "6"),
			details: []youtrack.Pair{
				{Key: "upstream_status", Value: number(200)},
				{Key: "upstream_body", Value: youtrack.NewString(attachmentFiled("ArticleAttachment", `"one.txt"`, "6"))},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, tc.body))

			_, err := client(t, server).Attachments.Create(t.Context(), tc.owner, attachmentFile("one.txt", "abcdef"),
				answeredWith("id"))

			want := youtrack.Error{
				Code:       youtrack.CodeUpstreamInvalid,
				AfterWrite: true,
				Details:    append([]youtrack.Pair{requestTo(http.MethodPost, server, tc.path+"?fields=id,name,size")}, tc.details...),
			}
			assert.Equal(t, want, errorOf(t, err))
		})
	}
}

func TestDeleteAttachmentRefusesAnIDThatIsNoInternalID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
	}{
		{name: "the readable id of an issue", id: "DEV-2"},
		{name: "two dots", id: ".."},
		{name: "empty", id: ""},
		{name: "a number and a dash", id: "12-"},
		{name: "a negative number", id: "-1"},
		{name: "a letter after the number", id: "12-2x"},
		{name: "the name of a file", id: "a-b.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client(t, fake.ServeNothing(t)).Attachments.Delete(t.Context(), "DEV-1", tc.id)
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}

func attachmentDeleting(t *testing.T, read http.HandlerFunc) *fake.Server {
	t.Helper()
	return fake.Serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			fake.JSON(http.StatusOK, "")(w, r)
			return
		}
		read(w, r)
	})
}

func TestDeleteAttachmentDeletesUnderTheOwnerTheReadNamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		owner    string
		read     string
		kind     string
		readable string
		targets  []string
	}{
		{
			name:     "an issue in lower case",
			owner:    "dev-7",
			read:     `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":{"$type":"Issue","idReadable":"DEV-7"}}`,
			kind:     "issue",
			readable: "DEV-7",
			targets: []string{
				"/api/issues/dev-7/attachments/12-5?fields=id,name,issue(idReadable)",
				"/api/issues/DEV-7/attachments/12-5?",
			},
		},
		{
			name:     "an article in mixed case",
			owner:    "dev-A-7",
			read:     `{"$type":"ArticleAttachment","id":"12-5","name":"a.txt","article":{"$type":"Article","idReadable":"DEV-A-7"}}`,
			kind:     "article",
			readable: "DEV-A-7",
			targets: []string{
				"/api/articles/dev-A-7/attachments/12-5?fields=id,name,article(idReadable)",
				"/api/articles/DEV-A-7/attachments/12-5?",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := attachmentDeleting(t, fake.JSON(http.StatusOK, tc.read))

			node, err := client(t, server).Attachments.Delete(t.Context(), tc.owner, "12-5")

			require.NoError(t, err)
			assert.Equal(t, youtrack.NewMap(
				youtrack.Pair{Key: "id", Value: youtrack.NewString("12-5")},
				youtrack.Pair{Key: "name", Value: youtrack.NewString("a.txt")},
				youtrack.Pair{Key: tc.kind, Value: youtrack.NewMap(
					youtrack.Pair{Key: "idReadable", Value: youtrack.NewString(tc.readable)})}), node)
			assert.Equal(t, tc.targets, server.Targets(t))
		})
	}
}

func TestDeleteAttachmentRefusesAnAnswerItCannotAddressTheDeletionBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		read string
	}{
		{
			name: "another attachment than the one asked for",
			read: `{"$type":"IssueAttachment","id":"12-6","name":"a.txt","issue":{"$type":"Issue","idReadable":"DEV-7"}}`,
		},
		{
			name: "an id that is no text",
			read: `{"$type":"IssueAttachment","id":125,"name":"a.txt","issue":{"$type":"Issue","idReadable":"DEV-7"}}`,
		},
		{
			name: "an owner of two dots",
			read: `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":{"$type":"Issue","idReadable":".."}}`,
		},
		{
			name: "an owner that is an article",
			read: `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":{"$type":"Issue","idReadable":"DEV-A-7"}}`,
		},
		{
			name: "an owner with no readable id",
			read: `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":{"$type":"Issue","idReadable":null}}`,
		},
		{
			name: "an owner that arrived as no object",
			read: `{"$type":"IssueAttachment","id":"12-5","name":"a.txt","issue":null}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := attachmentDeleting(t, fake.JSON(http.StatusOK, tc.read))

			_, err := client(t, server).Attachments.Delete(t.Context(), "DEV-7", "12-5")

			read := requestTo(http.MethodGet, server, "/api/issues/DEV-7/attachments/12-5?fields=id,name,issue(idReadable)")
			assert.Equal(t, unreadable(read, tc.read), errorOf(t, err))
			assert.Equal(t, []string{"/api/issues/DEV-7/attachments/12-5"}, server.Paths())
		})
	}
}

func TestListAttachmentsReadsTheOwnerThroughItsOwnAPI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		owner string
		path  string
	}{
		{name: "an issue", owner: "DEV-1", path: "/api/issues/DEV-1/attachments"},
		{name: "an article", owner: "DEV-A-1", path: "/api/articles/DEV-A-1/attachments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `[]`))

			_, err := client(t, server).Attachments.List(t.Context(), tc.owner, &youtrack.ListAttachmentsOptions{Fields: "id"})

			require.NoError(t, err)
			assert.Equal(t, []string{tc.path}, server.Paths())
		})
	}
}

func TestListAttachmentsResolvesTheLinkOfARecordTheServerNamedNothing(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `[{"url":"/api/files/12-2?sign=s"}]`))

	node, err := client(t, server).Attachments.List(t.Context(), "DEV-1", &youtrack.ListAttachmentsOptions{Fields: "url"})

	require.NoError(t, err)
	assert.Equal(t, wholePage("attachments", youtrack.NewMap(
		youtrack.Pair{Key: "url", Value: youtrack.NewString(server.Origin + "/api/files/12-2?sign=s")})), node)
}

func TestAttachmentsRefuseTheContentOfAFileInTheFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func(ctx context.Context, attachments *youtrack.AttachmentsService) error
	}{
		{
			name: "in a record of a list of attachments",
			call: func(ctx context.Context, attachments *youtrack.AttachmentsService) error {
				_, err := attachments.List(ctx, "DEV-1", &youtrack.ListAttachmentsOptions{Fields: "+base64Content"})
				return err
			},
		},
		{
			name: "in what an upload answers with",
			call: func(ctx context.Context, attachments *youtrack.AttachmentsService) error {
				_, err := attachments.Create(ctx, "DEV-1", attachmentFile("one.txt", "x"),
					answeredWith("id,base64Content"))
				return err
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.call(t.Context(), client(t, fake.ServeNothing(t)).Attachments)
			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}
