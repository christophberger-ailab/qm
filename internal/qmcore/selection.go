package qmcore

import (
	"fmt"
	"sort"
	"strings"
)

// This file models the project's render selection: the three independent
// Quarto profiles that together describe one artefact.
//
// A render is addressed by three orthogonal axes, each a profile of its own:
//
//	topic-<t>     what is rendered   (which content folder, or all of them)
//	format-<f>    how it is rendered (book/website/slides, output-dir)
//	audience-<a>  who it is for      (which ::: pol / ::: fw content)
//
//	quarto render --profile topic-calltaker,format-handout,audience-pol
//
// The axes are derived from the profile name *prefix*, not from a suffix
// table, so a new audience is a new `_quarto-audience-<x>.yml` and nothing
// else. The old scheme encoded topic and audience in one name
// (`calltaker-pol`) and could therefore compose an output path from two of
// the three axes at most.

// Axis is one of the three dimensions a render is selected along.
type Axis string

const (
	AxisTopic    Axis = "topic"
	AxisFormat   Axis = "format"
	AxisAudience Axis = "audience"
)

// Axes lists the axes in the canonical order profiles are passed to Quarto.
// The order matters: Quarto merges profiles left to right and the
// first-listed profile wins for scalar keys, so `format` has to precede
// `audience` for a book's output-dir to come from the format.
var Axes = []Axis{AxisTopic, AxisFormat, AxisAudience}

// Prefix returns the profile-name prefix of the axis, e.g. "topic-".
func (a Axis) Prefix() string { return string(a) + "-" }

// ProfileName returns the full profile name for a value on the axis,
// e.g. AxisTopic.ProfileName("calltaker") == "topic-calltaker".
func (a Axis) ProfileName(value string) string { return a.Prefix() + value }

// AllTopics and NoTopic are the two spellings of the topic that selects no
// single content folder. They mean the same thing seen from two sides: the
// render covers the project as a whole rather than one topic's folder.
//
// It is what a website render uses — the site is the whole tree, so every
// topic is in it (`all`) and none of them is *the* one (`none`). Neither
// value names a folder, and looking for an `all/` or `none/` directory is
// the bug this constant exists to prevent.
const (
	AllTopics = "all"
	NoTopic   = "none"
)

// IsWholeProject reports whether the topic value addresses the project as a
// whole instead of one content folder. Such a topic has no folder to
// flatten: the build documents are emptied and Quarto's own `project:
// render:` list decides what is rendered.
func IsWholeProject(topic string) bool {
	return topic == AllTopics || topic == NoTopic
}

// WholeProjectTopics lists the whole-project topic values, for error
// messages that have to name them.
func WholeProjectTopics() []string { return []string{AllTopics, NoTopic} }

// Selection is one topic × format × audience combination — exactly one
// value per axis, which is what a single `quarto render` produces.
type Selection struct {
	Topic    string
	Format   string
	Audience string
}

// Get returns the selection's value on the given axis.
func (s Selection) Get(a Axis) string {
	switch a {
	case AxisTopic:
		return s.Topic
	case AxisFormat:
		return s.Format
	case AxisAudience:
		return s.Audience
	}
	return ""
}

// set assigns the value on the given axis.
func (s *Selection) set(a Axis, value string) {
	switch a {
	case AxisTopic:
		s.Topic = value
	case AxisFormat:
		s.Format = value
	case AxisAudience:
		s.Audience = value
	}
}

// ProfileNames returns the three profile names in canonical order.
func (s Selection) ProfileNames() []string {
	out := make([]string, 0, len(Axes))
	for _, a := range Axes {
		out = append(out, a.ProfileName(s.Get(a)))
	}
	return out
}

// String is the value of Quarto's --profile flag for this selection.
func (s Selection) String() string { return strings.Join(s.ProfileNames(), ",") }

// ParseSelection turns a list of profile names into a Selection.
//
// It is the project's validation gate: Quarto itself accepts a misspelled
// profile silently (the missing `_quarto-<name>.yml` is ignored and every
// `{{< var >}}` it should have set resolves to `?var:name`, which then ends
// up in the output file name). Two profiles from the same group are not
// rejected by Quarto either — their configurations simply merge, and array
// keys such as `book: chapters` concatenate. Both cases are errors here.
//
// Profile names appended by Quarto's own group defaults are part of the
// input: a pre-render hook sees the effective QUARTO_PROFILE, not the one
// typed on the command line.
func ParseSelection(names []string) (Selection, error) {
	var sel Selection
	seen := map[Axis]string{}
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		axis, value, ok := SplitProfileName(name)
		if !ok {
			return sel, fmt.Errorf(
				"profile %q has no known axis prefix (expected one of %s)",
				name, prefixList())
		}
		if prev, dup := seen[axis]; dup {
			return sel, fmt.Errorf(
				"two %s profiles selected at once: %s and %s; "+
					"pick exactly one per axis",
				axis, axis.ProfileName(prev), name)
		}
		seen[axis] = value
		sel.set(axis, value)
	}
	var missing []string
	for _, a := range Axes {
		if _, ok := seen[a]; !ok {
			missing = append(missing, string(a))
		}
	}
	if len(missing) > 0 {
		return sel, fmt.Errorf(
			"no %s profile selected; a render needs one profile per axis "+
				"(%s), e.g. --profile %s",
			strings.Join(missing, " and no "), prefixList(),
			"topic-<t>,format-<f>,audience-<a>")
	}
	return sel, nil
}

// SplitProfileName takes a profile name apart into its axis and value.
// The name may carry the `_quarto-` file prefix.
func SplitProfileName(name string) (Axis, string, bool) {
	name = strings.TrimPrefix(name, "_quarto-")
	for _, a := range Axes {
		if v, ok := strings.CutPrefix(name, a.Prefix()); ok && v != "" {
			return a, v, true
		}
	}
	return "", "", false
}

// prefixList renders the axis prefixes for an error message.
func prefixList() string {
	out := make([]string, 0, len(Axes))
	for _, a := range Axes {
		out = append(out, a.Prefix()+"*")
	}
	return strings.Join(out, ", ")
}

// AxisValues lists the values an axis offers in the project: the names of
// the `_quarto-<axis>-<value>.yml` files, sorted.
func AxisValues(docRoot string, a Axis) ([]string, error) {
	files, err := DiscoverProfiles(docRoot)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		axis, value, ok := SplitProfileName(StripYamlExt(f))
		if ok && axis == a {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out, nil
}
