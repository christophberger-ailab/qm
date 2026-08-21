package qmcore

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Nothing writes `book: chapters:` any more: the list is a fixed one-element
// list in _quarto.yml, and the book's real structure comes from flattening
// the topic folder at render time. A generator for it would now be actively
// harmful, because Quarto concatenates array keys across configurations
// instead of replacing them.

// FindMappingValue returns the value node for the given key in a YAML
// mapping node, or nil if not found.
func FindMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// ReplaceMappingValue replaces the value for key in a YAML mapping node.
// Returns true if the key was found and replaced.
func ReplaceMappingValue(parent *yaml.Node, key string, newValue *yaml.Node) bool {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1] = newValue
			return true
		}
	}
	return false
}

// SetMappingScalar adds or replaces a scalar key/value pair in a YAML mapping.
func SetMappingScalar(parent *yaml.Node, key, value string) error {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return fmt.Errorf("SetMappingScalar: not a mapping node")
	}
	val := &yaml.Node{Kind: yaml.ScalarNode, Value: value}
	if !ReplaceMappingValue(parent, key, val) {
		k := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
		parent.Content = append(parent.Content, k, val)
	}
	return nil
}

// RemoveMappingKey removes the key (and its value) from a YAML mapping node.
// Returns true if the key was present.
func RemoveMappingKey(parent *yaml.Node, key string) bool {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
			return true
		}
	}
	return false
}

// RenameMappingKey renames a key in a YAML mapping node, preserving its value
// and position. Returns true if the key was found and renamed.
func RenameMappingKey(parent *yaml.Node, oldKey, newKey string) bool {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == oldKey {
			parent.Content[i].Value = newKey
			return true
		}
	}
	return false
}
