package config

import (
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"
)

// Entry is one named item of a qm.yml map, in file order.
type Entry[T any] struct {
	Name  string
	Value T
}

// Entries is a YAML mapping that keeps its keys' order: the order the
// formats, audiences and topics are written in is the order the web UI
// lists them and a render runs them. A key with an empty value (`std:`)
// gets T's zero value.
type Entries[T any] []Entry[T]

// UnmarshalYAML reads a mapping of name → T.
func (e *Entries[T]) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.SequenceNode {
		// A plain list of names is a mapping with empty values.
		for _, item := range n.Content {
			if item.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: want a name", item.Line)
			}
			*e = append(*e, Entry[T]{Name: item.Value})
		}
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: want a mapping of names", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		var val T
		if !(v.Kind == yaml.ScalarNode && (v.Tag == "!!null" || v.Value == "")) {
			if err := v.Decode(&val); err != nil {
				return fmt.Errorf("%s: %w", k.Value, err)
			}
		}
		if _, dup := e.Get(k.Value); dup {
			return fmt.Errorf("line %d: %s is defined twice", k.Line, k.Value)
		}
		*e = append(*e, Entry[T]{Name: k.Value, Value: val})
	}
	return nil
}

// Get returns the entry called name.
func (e Entries[T]) Get(name string) (T, bool) {
	for _, x := range e {
		if x.Name == name {
			return x.Value, true
		}
	}
	var zero T
	return zero, false
}

// Names lists the entry names in order.
func (e Entries[T]) Names() []string {
	out := make([]string, len(e))
	for i, x := range e {
		out[i] = x.Name
	}
	return out
}

// Has reports whether an entry called name exists.
func (e Entries[T]) Has(name string) bool {
	return slices.ContainsFunc(e, func(x Entry[T]) bool { return x.Name == name })
}

// StringList is a YAML value that is either one string or a list of them.
type StringList []string

// UnmarshalYAML accepts `x` and `[x, y]`.
func (s *StringList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*s = StringList{n.Value}
		return nil
	}
	var l []string
	if err := n.Decode(&l); err != nil {
		return err
	}
	*s = l
	return nil
}

// MarshalYAML writes a one-element list as a plain string.
func (s StringList) MarshalYAML() (any, error) {
	if len(s) == 1 {
		return s[0], nil
	}
	return []string(s), nil
}
