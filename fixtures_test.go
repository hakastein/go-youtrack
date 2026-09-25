package youtrack_test

import (
	"cmp"
	"encoding/json"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack/fake"
)

const (
	projectPath   = "/api/admin/projects/DEV"
	issuePath     = "/api/issues/DEV-1"
	fieldNaming   = "field(name,localizedName,fieldType(valueType,isMultiValue))"
	projectFields = "id,shortName,customFields(id,ordinal,canBeEmpty," + fieldNaming + ")"
	issueFields   = "id,idReadable,summary,description,project(id,shortName,name),customFields(name,value(name,login,minutes,text,id,localizedName)," +
		"projectCustomField(id,ordinal,field(localizedName,fieldType(valueType,isMultiValue))))," +
		"links(direction,linkType(name,sourceToTarget,targetToSource),issues(id,idReadable))"
	issueToWriteFields = "idReadable,customFields($type,name,projectCustomField(id)),project(" + projectFields + ")"
)

type metaField struct {
	id        string
	name      string
	localized string
	valueType string
	multi     bool
	required  bool
	ordinal   string
}

func (f metaField) naming() string {
	localized := "null"
	if f.localized != "" {
		localized = strconv.Quote(f.localized)
	}
	return `{"$type":"CustomField","name":` + strconv.Quote(f.name) + `,"localizedName":` + localized +
		`,"fieldType":{"$type":"FieldType","valueType":` + strconv.Quote(f.valueType) +
		`,"isMultiValue":` + strconv.FormatBool(f.multi) + `}}`
}

func (f metaField) json() string {
	return `{"$type":"ProjectCustomField","id":` + strconv.Quote(f.id) +
		`,"ordinal":` + cmp.Or(f.ordinal, "0") +
		`,"canBeEmpty":` + strconv.FormatBool(!f.required) +
		`,"field":` + f.naming() + `}`
}

// The answer to a field of the project: its naming, whether it may stand empty, and the bundle when the test gives one.
func (f metaField) answer(bundle string) string {
	held := `{"$type":"ProjectCustomField","id":` + strconv.Quote(f.id) + `,"canBeEmpty":` + strconv.FormatBool(!f.required) + `,"field":` + f.naming()
	if bundle == "" {
		return held + `}`
	}
	return held + `,"bundle":` + bundle + `}`
}

func enumBundle(values ...string) string {
	return `{"$type":"EnumBundle","id":"5-1","values":[` + strings.Join(values, ",") + `]}`
}

func bundleValue(id, name string, archived bool) string {
	return `{"$type":"EnumBundleElement","id":` + strconv.Quote(id) + `,"name":` + strconv.Quote(name) + `,"archived":` + strconv.FormatBool(archived) + `}`
}

func enumField(id, name string) metaField {
	return metaField{id: id, name: name, valueType: "enum"}
}

func projectJSON(fields ...metaField) string {
	metadata := make([]string, 0, len(fields))
	for _, f := range fields {
		metadata = append(metadata, f.json())
	}
	return `{"$type":"Project","id":"0-1","shortName":"DEV","customFields":[` + strings.Join(metadata, ",") + `]}`
}

type heldValue struct {
	name      string
	localized string
	valueType string
	multi     bool
	value     string
	ordinal   string
	binding   string
}

func (v heldValue) json() string {
	localized := "null"
	if v.localized != "" {
		localized = strconv.Quote(v.localized)
	}
	return `{"$type":"IssueCustomField","name":` + strconv.Quote(v.name) + `,"value":` + cmp.Or(v.value, "null") +
		`,"projectCustomField":{"$type":"ProjectCustomField","id":` + strconv.Quote(cmp.Or(v.binding, "1-1")) +
		`,"ordinal":` + cmp.Or(v.ordinal, "0") +
		`,"field":{"$type":"CustomField","localizedName":` + localized + `,"fieldType":{"$type":"FieldType","valueType":` +
		strconv.Quote(v.valueType) + `,"isMultiValue":` + strconv.FormatBool(v.multi) + `}}}}`
}

func customFieldsJSON(values ...heldValue) json.RawMessage {
	held := make([]string, 0, len(values))
	for _, v := range values {
		held = append(held, v.json())
	}
	return json.RawMessage("[" + strings.Join(held, ",") + "]")
}

func element(name string) string {
	return `{"$type":"EnumBundleElement","id":"3-` + strconv.Itoa(len(name)) + `","name":` + strconv.Quote(name) + `}`
}

func issueJSON(t *testing.T, members map[string]any) string {
	t.Helper()
	issue := map[string]any{
		"$type":       "Issue",
		"id":          "2-1",
		"idReadable":  "DEV-1",
		"summary":     "First",
		"description": nil,
		"project":     map[string]any{"$type": "Project", "id": "0-1", "shortName": "DEV", "name": "Development"},
		"links":       []any{},
	}
	maps.Copy(issue, members)
	answer, err := json.Marshal(issue)
	require.NoError(t, err)
	return string(answer)
}

type heldClass struct {
	name    string
	class   string
	binding string
}

func classesJSON(held ...heldClass) string {
	fields := make([]string, 0, len(held))
	for _, f := range held {
		fields = append(fields, `{"$type":`+strconv.Quote(f.class)+`,"name":`+strconv.Quote(f.name)+
			`,"projectCustomField":{"$type":"ProjectCustomField","id":`+strconv.Quote(f.binding)+`}}`)
	}
	return "[" + strings.Join(fields, ",") + "]"
}

func issueToWriteJSON(project, classes string) string {
	return `{"$type":"Issue","idReadable":"DEV-1","customFields":` + classes + `,"project":` + project + `}`
}

func servingWrite(t *testing.T, project, classes string, write http.HandlerFunc) *fake.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+issuePath, fake.JSON(http.StatusOK, issueToWriteJSON(project, classes)))
	mux.HandleFunc("POST "+issuePath, write)
	return fake.Serve(t, mux.ServeHTTP)
}
