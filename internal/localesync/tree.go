package localesync

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
)

type node struct {
	keys     []string
	children map[string]*node
	spaced   map[string]bool
	isInline bool
	raw      json.RawMessage
}

func (n *node) isObject() bool {
	return n != nil && n.children != nil
}

func (n *node) child(key string) *node {
	if !n.isObject() {
		return nil
	}
	return n.children[key]
}

func (n *node) add(key string, child *node, isSpaced bool) {
	if _, exists := n.children[key]; !exists {
		n.keys = append(n.keys, key)
	}
	n.children[key] = child
	n.spaced[key] = isSpaced
}

func emptyObject() *node {
	return &node{children: map[string]*node{}, spaced: map[string]bool{}}
}

func parseTree(data []byte) (*node, error) {
	trimmed := bytes.TrimSpace(data)
	if !json.Valid(trimmed) {
		return nil, errors.New("invalid JSON")
	}
	return parseValidTree(trimmed), nil
}

func parseValidTree(value []byte) *node {
	if value[0] != '{' {
		return &node{raw: bytes.Clone(value)}
	}

	decoder := json.NewDecoder(bytes.NewReader(value))
	_, _ = decoder.Token()
	object := emptyObject()
	object.isInline = !bytes.Contains(value, []byte("\n"))
	offset := decoder.InputOffset()
	for decoder.More() {
		token, _ := decoder.Token()
		key, _ := token.(string)
		gap := value[offset:decoder.InputOffset()]
		isSpaced := bytes.Count(gap[:bytes.IndexByte(gap, '"')], []byte("\n")) > 1
		var child json.RawMessage
		_ = decoder.Decode(&child)
		object.add(key, parseValidTree(child), isSpaced)
		offset = decoder.InputOffset()
	}
	return object
}

func encodeTree(root *node) []byte {
	var buffer bytes.Buffer
	writeNode(&buffer, root, 0)
	buffer.WriteByte('\n')
	return buffer.Bytes()
}

func writeNode(buffer *bytes.Buffer, n *node, depth int) {
	switch {
	case !n.isObject():
		buffer.Write(n.raw)
	case len(n.keys) == 0:
		buffer.WriteString("{}")
	case n.isInline:
		writeInlineObject(buffer, n, depth)
	default:
		writeBlockObject(buffer, n, depth)
	}
}

func writeBlockObject(buffer *bytes.Buffer, n *node, depth int) {
	buffer.WriteString("{\n")
	for i, key := range n.keys {
		if n.spaced[key] {
			buffer.WriteByte('\n')
		}
		buffer.WriteString(strings.Repeat("  ", depth+1))
		writeString(buffer, key)
		buffer.WriteString(": ")
		writeNode(buffer, n.children[key], depth+1)
		if i < len(n.keys)-1 {
			buffer.WriteByte(',')
		}
		buffer.WriteByte('\n')
	}
	buffer.WriteString(strings.Repeat("  ", depth))
	buffer.WriteByte('}')
}

func writeInlineObject(buffer *bytes.Buffer, n *node, depth int) {
	buffer.WriteString("{ ")
	for i, key := range n.keys {
		if i > 0 {
			buffer.WriteString(", ")
		}
		writeString(buffer, key)
		buffer.WriteString(": ")
		writeNode(buffer, n.children[key], depth)
	}
	buffer.WriteString(" }")
}

func writeString(buffer *bytes.Buffer, value string) {
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	buffer.Truncate(buffer.Len() - 1)
}

func equalNodes(a, b *node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.isObject() != b.isObject() {
		return false
	}
	if !a.isObject() {
		return equalLeaves(a.raw, b.raw)
	}
	if len(a.children) != len(b.children) {
		return false
	}
	for key, child := range a.children {
		if !equalNodes(child, b.children[key]) {
			return false
		}
	}
	return true
}

func equalLeaves(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var left, right any
	_ = json.Unmarshal(a, &left)
	_ = json.Unmarshal(b, &right)
	return reflect.DeepEqual(left, right)
}
