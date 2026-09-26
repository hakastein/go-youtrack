package youtrack_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hakastein/youtrack"
)

func TestNodeHoldsWhatItWasBuiltOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		node  *youtrack.Node
		kind  youtrack.Kind
		value string
	}{
		{name: "null", node: youtrack.NewNull(), kind: youtrack.NullNode},
		{name: "a string", node: youtrack.NewString("DEV-1"), kind: youtrack.StringNode, value: "DEV-1"},
		{name: "a number", node: youtrack.NewNumber("1.50"), kind: youtrack.NumberNode, value: "1.50"},
		{name: "true", node: youtrack.NewBool(true), kind: youtrack.BoolNode, value: "true"},
		{name: "false", node: youtrack.NewBool(false), kind: youtrack.BoolNode, value: "false"},
		{name: "prose", node: youtrack.NewText("First\nSecond"), kind: youtrack.TextNode, value: "First\nSecond"},
		{name: "a list", node: youtrack.NewList(youtrack.NewNull()), kind: youtrack.ListNode},
		{name: "a map", node: youtrack.NewMap(), kind: youtrack.MapNode},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.kind, tc.node.Kind())
			assert.Equal(t, tc.value, tc.node.Value())
		})
	}
}

func TestNodeKeepsTheOrderOfItsItemsAndPairs(t *testing.T) {
	t.Parallel()
	first, second := youtrack.NewString("First"), youtrack.NewString("Second")
	pairs := []youtrack.Pair{{Key: "second", Value: second}, youtrack.DataPair("First", first)}

	assert.Equal(t, []*youtrack.Node{second, first}, youtrack.NewList(second, first).Items())
	assert.Equal(t, pairs, youtrack.NewMap(pairs...).Pairs())
}

func TestAnEmptyNodeIsOneHoweverItWasBuilt(t *testing.T) {
	t.Parallel()

	assert.Equal(t, youtrack.NewList(), youtrack.NewList([]*youtrack.Node{}...))
	assert.Equal(t, youtrack.NewMap(), youtrack.NewMap([]youtrack.Pair{}...))
}

func TestNodeIsNotChangedByWhatItWasBuiltOfOrWhatItHandsOut(t *testing.T) {
	t.Parallel()
	items := []*youtrack.Node{youtrack.NewString("First")}
	list := youtrack.NewList(items...)

	items[0] = youtrack.NewString("Second")
	list.Items()[0] = youtrack.NewString("Third")

	assert.Equal(t, []*youtrack.Node{youtrack.NewString("First")}, list.Items())
}

func TestLookupFindsTheValueUnderAKeyOfAMap(t *testing.T) {
	t.Parallel()
	found := youtrack.NewString("DEV-1")
	tests := []struct {
		name  string
		node  *youtrack.Node
		key   string
		want  *youtrack.Node
		found bool
	}{
		{name: "a key of the map", node: youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: found}), key: "idReadable",
			want: found, found: true},
		{name: "a key from the data", node: youtrack.NewMap(youtrack.DataPair("Статус", found)), key: "Статус",
			want: found, found: true},
		{name: "a key the map lacks", node: youtrack.NewMap(youtrack.Pair{Key: "idReadable", Value: found}), key: "summary"},
		{name: "no map", node: youtrack.NewList(found), key: "idReadable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tc.node.Lookup(tc.key)

			assert.Equal(t, tc.found, ok)
			assert.Same(t, tc.want, got)
		})
	}
}

func TestNodeMarshalsToJSONInItsOrder(t *testing.T) {
	t.Parallel()
	node := youtrack.NewMap(
		youtrack.Pair{Key: "summary", Value: youtrack.NewString("<First> & \"Second\"")},
		youtrack.DataPair("Статус", youtrack.NewList(youtrack.NewNumber("1.50"), youtrack.NewBool(true), youtrack.NewNull())),
		youtrack.Pair{Key: "description", Value: youtrack.NewText("First\nSecond")},
		youtrack.Pair{Key: "links", Value: youtrack.NewMap()},
		youtrack.Pair{Key: "tags", Value: youtrack.NewList()},
	)

	written, err := json.Marshal(node)

	require.NoError(t, err)
	assert.Equal(t, `{"summary":"\u003cFirst\u003e \u0026 \"Second\"","Статус":[1.50,true,null],"description":"First\nSecond","links":{},"tags":[]}`, string(written))
}

func TestNodeRefusesToMarshalWhatHoldsNoValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		node *youtrack.Node
	}{
		{name: "a nil value under a key", node: youtrack.NewMap(youtrack.Pair{Key: "summary"})},
		{name: "a zero node in a list", node: youtrack.NewList(&youtrack.Node{})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := json.Marshal(tc.node)

			assert.Error(t, err)
		})
	}
}

func TestCheckKeyTakesANameOfTheModulesOwn(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"idReadable", "$type", "_links", "issue2", "Nope"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			assert.NoError(t, youtrack.CheckKey(key))
		})
	}
}

func TestCheckKeyRefusesWhatAReaderOfYAMLTakesForSomethingElse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
	}{
		{name: "nothing", key: ""},
		{name: "a leading digit", key: "2issue"},
		{name: "a dash", key: "id-readable"},
		{name: "a letter outside ASCII", key: "Статус"},
		{name: "a bool of YAML 1.1", key: "Yes"},
		{name: "null", key: "null"},
		{name: "a one-letter bool", key: "n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Error(t, youtrack.CheckKey(tc.key))
		})
	}
}
