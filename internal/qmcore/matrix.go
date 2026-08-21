package qmcore

import (
	"fmt"
	"slices"
	"strings"
)

// This file builds the render matrix: which topic × format × audience
// combinations the project actually has.
//
// The full cross product is not it. A book has no website format, the
// systemadministration topic has no agency variants, and the *Spickzettel*
// has no slide deck. Rather than encode those exceptions in a driver
// script — which is where they lived before, in render.ps1 — the profiles
// declare the axes they take part in:
//
//	# _quarto-topic-sysadmin.yml
//	qm:
//	  formats:   [handout-no-tutorials, handbook, slides]
//	  audiences: [std]
//
//	# _quarto-format-website-full.yml   (the authors' complete site)
//	qm:
//	  audiences: [all]
//
// A profile that declares nothing takes part in everything the project
// offers. Where a topic and a format both declare an axis, both have to
// agree — the combination has to make sense from either side.

// Matrix is a render matrix request. Empty fields mean "everything the
// project offers on that axis"; a non-empty field restricts it.
type Matrix struct {
	Topics    []string
	Formats   []string
	Audiences []string
}

// BuildMatrix expands the request into the selections to render, in a
// stable order: topics outermost, then formats, then audiences.
//
// A name that no profile file backs is an error rather than a silently
// empty result: `qm render --topic calltakr` should say so.
func BuildMatrix(docRoot string, req Matrix) ([]Selection, error) {
	available := map[Axis][]string{}
	for _, a := range Axes {
		vals, err := AxisValues(docRoot, a)
		if err != nil {
			return nil, err
		}
		if len(vals) == 0 {
			return nil, fmt.Errorf(
				"%s defines no %s profile (_quarto-%s*.yml)", docRoot, a, a.Prefix())
		}
		available[a] = vals
	}

	requested := map[Axis][]string{
		AxisTopic:    req.Topics,
		AxisFormat:   req.Formats,
		AxisAudience: req.Audiences,
	}
	for _, a := range Axes {
		if err := checkKnown(a, requested[a], available[a]); err != nil {
			return nil, err
		}
	}

	topics := requested[AxisTopic]
	if len(topics) == 0 {
		topics = available[AxisTopic]
	}

	var out []Selection
	for _, topic := range topics {
		tp, err := LoadProfile(docRoot, AxisTopic.ProfileName(topic))
		if err != nil {
			return nil, err
		}
		formats, err := axisFor(AxisFormat, tp, tp.QM.Formats,
			requested[AxisFormat], available[AxisFormat])
		if err != nil {
			return nil, err
		}
		for _, f := range formats {
			fp, err := LoadProfile(docRoot, AxisFormat.ProfileName(f))
			if err != nil {
				return nil, err
			}
			// The format may exclude this topic outright.
			if len(fp.QM.Topics) > 0 && !slices.Contains(fp.QM.Topics, topic) {
				continue
			}
			audiences, err := axisFor(AxisAudience, tp, tp.QM.Audiences,
				requested[AxisAudience], available[AxisAudience])
			if err != nil {
				return nil, err
			}
			if len(fp.QM.Audiences) > 0 {
				if err := checkKnown(AxisAudience, fp.QM.Audiences, available[AxisAudience]); err != nil {
					return nil, fmt.Errorf("%s: qm: audiences: %w", fp.Path, err)
				}
				audiences = intersect(audiences, fp.QM.Audiences)
			}
			for _, a := range audiences {
				out = append(out, Selection{Topic: topic, Format: f, Audience: a})
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf(
			"the selection is empty: no combination takes part in all of it. " +
				"Check the qm: formats: and qm: audiences: of the profiles")
	}
	return out, nil
}

// intersect keeps the entries of a that b also has, in a's order.
func intersect(a, b []string) []string {
	var out []string
	for _, v := range a {
		if slices.Contains(b, v) {
			out = append(out, v)
		}
	}
	return out
}

// axisFor narrows one axis for one topic: what the topic declares, cut down
// to what the caller asked for, in the project's own order.
func axisFor(a Axis, topic *Profile, declared, requested, available []string) ([]string, error) {
	allowed := available
	if len(declared) > 0 {
		if err := checkKnown(a, declared, available); err != nil {
			return nil, fmt.Errorf("%s: qm: %ss: %w", topic.Path, a, err)
		}
		allowed = declared
	}
	if len(requested) == 0 {
		return inOrder(allowed, available), nil
	}
	var out []string
	for _, v := range available {
		if slices.Contains(allowed, v) && slices.Contains(requested, v) {
			out = append(out, v)
		}
	}
	return out, nil
}

// inOrder returns sel sorted the way available lists it, so that the render
// order does not depend on how a profile happened to spell its list.
func inOrder(sel, available []string) []string {
	var out []string
	for _, v := range available {
		if slices.Contains(sel, v) {
			out = append(out, v)
		}
	}
	return out
}

// checkKnown reports the first name on the axis that no profile backs.
func checkKnown(a Axis, names, available []string) error {
	for _, n := range names {
		if !slices.Contains(available, n) {
			return fmt.Errorf("unknown %s %q; the project has: %s",
				a, n, strings.Join(available, ", "))
		}
	}
	return nil
}
