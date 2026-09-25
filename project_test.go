package youtrack_test

import (
	"net/http"
	"testing"

	"github.com/hakastein/youtrack"
	"github.com/hakastein/youtrack/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShowProjectTakesWhitespaceAroundTheAnswer(t *testing.T) {
	t.Parallel()
	server := fake.Serve(t, fake.JSON(http.StatusOK, " \t\r\n"+`{"$type":"Project","shortName":"DEV"}`+" \t\r\n"))
	node, err := client(t, server).Projects.Show(t.Context(), "DEV", &youtrack.ShowProjectOptions{Fields: "shortName"})

	require.NoError(t, err)
	assert.Equal(t, youtrack.NewMap(youtrack.Pair{Key: "shortName", Value: youtrack.NewString("DEV")}), node)
}
