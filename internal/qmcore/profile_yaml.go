package qmcore

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// UpdateProfileYaml reads the profile yaml, sets book.chapters to the given
// list (creating the file/section if missing), and writes it back.
func UpdateProfileYaml(profilePath string, chapters []string) error {
	var root yaml.Node

	data, err := os.ReadFile(profilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot read %q: %w", profilePath, err)
	}

	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("cannot parse %q: %w", profilePath, err)
		}
	}

	if root.Kind == 0 {
		root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
			{Kind: yaml.MappingNode},
		}}
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return fmt.Errorf("unexpected yaml structure in %q", profilePath)
	}
	mappingRoot := root.Content[0]

	bookNode := FindMappingValue(mappingRoot, "book")
	if bookNode == nil {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "book"}
		bookNode = &yaml.Node{Kind: yaml.MappingNode}
		mappingRoot.Content = append(mappingRoot.Content, keyNode, bookNode)
	}

	chaptersSeq := &yaml.Node{Kind: yaml.SequenceNode}
	for _, ch := range chapters {
		chaptersSeq.Content = append(chaptersSeq.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: ch,
		})
	}

	if !ReplaceMappingValue(bookNode, "chapters", chaptersSeq) {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "chapters"}
		bookNode.Content = append(bookNode.Content, keyNode, chaptersSeq)
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("cannot marshal yaml: %w", err)
	}

	return os.WriteFile(profilePath, out, 0644)
}

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
