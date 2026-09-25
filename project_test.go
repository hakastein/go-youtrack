package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShowProjectReadsTheDefaultFields(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Project","shortName":"DEV","name":"First",`+
		`"plugins":{"timeTrackingSettings":{"enabled":true,"workItemTypes":[{"name":"Development"}]}}}`))

	node, err := client(t, server).Projects.Show(t.Context(), "DEV", nil)

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(
		youtrack.Pair{Key: "shortName", Value: youtrack.NewString("DEV")},
		youtrack.Pair{Key: "name", Value: youtrack.NewString("First")},
		youtrack.Pair{Key: "plugins", Value: youtrack.NewMap(youtrack.Pair{Key: "timeTrackingSettings", Value: youtrack.NewMap(
			youtrack.Pair{Key: "enabled", Value: youtrack.NewBool(true)},
			youtrack.Pair{Key: "workItemTypes", Value: youtrack.NewList(youtrack.NewMap(
				youtrack.Pair{Key: "name", Value: youtrack.NewString("Development")}))},
		)})},
	), node)
	assert.Equal(t, []string{youtrack.ProjectShowFields}, server.Fields())
}

func TestShowProjectSendsEveryFormOfACodeAsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code string
	}{
		{name: "upper case", code: "DEV"},
		{name: "lower case", code: "dev"},
		{name: "digits and an underscore after the first letter", code: "Dev_32"},
		{name: "letters outside ASCII", code: "ДЕВ"},
		{name: "a number that is no decimal digit", code: "D²"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := fake.Serve(t, fake.JSON(http.StatusOK, `{"$type":"Project","shortName":"DEV"}`))

			_, err := client(t, server).Projects.Show(t.Context(), tc.code, &youtrack.ShowProjectOptions{Fields: "shortName"})

			require.NoError(t, err)
			assert.Equal(t, "/api/admin/projects/"+tc.code, server.Request(t, 0).URL.Path)
		})
	}
}

func TestShowProjectRefusesACodeOfAnotherForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code string
	}{
		{name: "empty", code: ""},
		{name: "two dots", code: ".."},
		{name: "a digit first", code: "1DEV"},
		{name: "an underscore first", code: "_DEV"},
		{name: "a slash after the first letter", code: "a/b"},
		{name: "the readable id of an issue", code: "DEV-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projects := client(t, fake.ServeNothing(t)).Projects

			_, err := projects.Show(t.Context(), tc.code, &youtrack.ShowProjectOptions{Fields: "shortName"})

			assert.Equal(t, youtrack.Error{Code: youtrack.CodeBadUsage}, errorOf(t, err))
		})
	}
}
