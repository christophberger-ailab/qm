package project

import "go.yaml.in/yaml/v4"

// hasBook reports whether the yaml document contains a top-level book key.
func hasBook(src []byte) bool {
	var doc map[string]any
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return false
	}
	_, ok := doc["book"]
	return ok
}
