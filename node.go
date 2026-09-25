package youtrack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Kind is what a node holds.
type Kind int

const (
	// NullNode is a value the server sent as null or a counter it did not name.
	NullNode Kind = iota + 1
	// StringNode is a string of one line in spirit: a name, an id, a moment, a period.
	StringNode
	// NumberNode is a number as the server wrote it.
	NumberNode
	// BoolNode is true or false.
	BoolNode
	// TextNode is prose that may span lines: a description, the text of a comment, the content of an article.
	TextNode
	// ListNode is a list of nodes in the server's order or the module's.
	ListNode
	// MapNode is a mapping of keys to nodes in the order the fields expression named them.
	MapNode
)

func (k Kind) String() string {
	switch k {
	case NullNode:
		return "null"
	case StringNode:
		return "string"
	case NumberNode:
		return "number"
	case BoolNode:
		return "bool"
	case TextNode:
		return "text"
	case ListNode:
		return "list"
	case MapNode:
		return "map"
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// Node is one value of a document, the answer of an operation: every decision about the data is taken when the
// node is built, so whoever prints it only writes bytes. The zero Node holds no value of any kind.
type Node struct {
	kind  Kind
	value string
	items []*Node
	pairs []Pair
}

// Pair is one key of a map node and its value. FromData says the key is a name from the server's data, as the
// name of a custom field or a link phrase, rather than a name of the module's own that CheckKey holds to a grammar.
type Pair struct {
	Key      string
	Value    *Node
	FromData bool
}

// DataPair is a pair whose key is a name from the server's data.
func DataPair(key string, value *Node) Pair {
	return Pair{Key: key, Value: value, FromData: true}
}

// NewNull is a null node.
func NewNull() *Node {
	return &Node{kind: NullNode}
}

// NewString is a string node.
func NewString(s string) *Node {
	return &Node{kind: StringNode, value: s}
}

// NewNumber is a number node holding n as written.
func NewNumber(n json.Number) *Node {
	return &Node{kind: NumberNode, value: string(n)}
}

// NewBool is a bool node.
func NewBool(b bool) *Node {
	return &Node{kind: BoolNode, value: strconv.FormatBool(b)}
}

// NewText is a node of prose that may span lines.
func NewText(s string) *Node {
	return &Node{kind: TextNode, value: s}
}

// NewList is a list node of the items in order.
func NewList(items ...*Node) *Node {
	return &Node{kind: ListNode, items: slices.Clone(items)}
}

// NewMap is a map node of the pairs in order.
func NewMap(pairs ...Pair) *Node {
	return &Node{kind: MapNode, pairs: slices.Clone(pairs)}
}

// Kind is what the node holds.
func (n *Node) Kind() Kind {
	return n.kind
}

// Value is a scalar as text: the string or the prose itself, the number as the server wrote it, true or false.
// It is empty for null, a list and a map.
func (n *Node) Value() string {
	return n.value
}

// Items are the nodes of a list; nil for any other kind.
func (n *Node) Items() []*Node {
	return slices.Clone(n.items)
}

// Pairs are the pairs of a map in order; nil for any other kind.
func (n *Node) Pairs() []Pair {
	return slices.Clone(n.pairs)
}

// Lookup is the value of a map under the key; false when the node is no map or holds no such key.
func (n *Node) Lookup(key string) (*Node, bool) {
	for _, pair := range n.pairs {
		if pair.Key == key {
			return pair.Value, true
		}
	}
	return nil, false
}

// MarshalJSON writes the node as JSON with the keys of a map in its order; a string and prose alike are strings.
func (n *Node) MarshalJSON() ([]byte, error) {
	var written bytes.Buffer
	if err := n.writeJSON(&written); err != nil {
		return nil, err
	}
	return written.Bytes(), nil
}

func (n *Node) writeJSON(w *bytes.Buffer) error {
	if n == nil {
		return errors.New("a nil node is no value")
	}
	switch n.kind {
	case NullNode:
		w.WriteString("null")
	case NumberNode, BoolNode:
		w.WriteString(n.value)
	case StringNode, TextNode:
		return writeJSONString(w, n.value)
	case ListNode:
		w.WriteByte('[')
		for i, item := range n.items {
			if i > 0 {
				w.WriteByte(',')
			}
			if err := item.writeJSON(w); err != nil {
				return err
			}
		}
		w.WriteByte(']')
	case MapNode:
		w.WriteByte('{')
		for i, pair := range n.pairs {
			if i > 0 {
				w.WriteByte(',')
			}
			if err := writeJSONString(w, pair.Key); err != nil {
				return err
			}
			w.WriteByte(':')
			if err := pair.Value.writeJSON(w); err != nil {
				return fmt.Errorf("under %s: %w", quote(pair.Key), err)
			}
		}
		w.WriteByte('}')
	default:
		return fmt.Errorf("a node of %s is no value", n.kind)
	}
	return nil
}

func writeJSONString(w *bytes.Buffer, s string) error {
	written, err := json.Marshal(s)
	w.Write(written)
	return err
}

// CheckKey says whether key is a name of the module's own: ASCII letters, digits, _ and $, not starting with a
// digit, and no word a YAML 1.1 reader takes for a bool or null. A fields expression names only such keys, and a
// key from the data is a DataPair instead.
func CheckKey(key string) error {
	for i, c := range []byte(key) {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', c == '_', c == '$':
		case '0' <= c && c <= '9' && i > 0:
		case '0' <= c && c <= '9':
			return fmt.Errorf("the key %s starts with a digit", quote(key))
		default:
			return fmt.Errorf("the key %s holds more than ASCII letters, digits, _ and $", quote(key))
		}
	}
	switch strings.ToLower(key) {
	case "", "null", "true", "false", "yes", "no", "on", "off", "y", "n":
		return fmt.Errorf("the key %s reads as a bool or null", quote(key))
	}
	return nil
}

// A quoted name stands in prose that is itself a double-quoted string once printed, so backticks keep it readable.
func quote(s string) string {
	goQuoted := strconv.Quote(s)
	escaped := strings.ReplaceAll(goQuoted[1:len(goQuoted)-1], `\"`, `"`)
	return "`" + strings.ReplaceAll(escaped, "`", "\\`") + "`"
}
