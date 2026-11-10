package config

import (
	"fmt"
	"slices"
)

// Request selects part of the matrix. An empty field means "everything the
// project offers on that axis".
type Request struct {
	Topics, Formats, Audiences []string
}

// AllTopics is the topic that stands for every topic of the project. It has
// no folder of its own. Requesting it selects every topic; declaring it in
// qm.yml (with its own formats and audiences) puts every topic into the
// websites it takes part in.
const AllTopics = "all"

// Expand returns the selections a request addresses, in a stable order:
// formats outermost, then topics, then audiences.
//
// A document format yields one selection per topic and audience. A website
// covers several topics at once, so it yields one selection per audience,
// with every requested topic in Selection.Topics.
//
// Topics and formats restrict each other: a topic that lists its formats
// takes part only in those, and the same holds for audiences and for a
// format's own lists. A name that the project does not define is an error.
//
// The topic "all" (AllTopics) is not a folder. In a request it means every
// topic. Declared in qm.yml, it makes every topic part of each website
// format and audience the "all" topic itself takes part in, whatever the
// individual topics restrict themselves to.
func (c *Config) Expand(req Request) ([]Selection, error) {
	if slices.Contains(req.Topics, AllTopics) {
		req.Topics = nil
	}
	for _, n := range req.Topics {
		if !c.Topics.Has(n) {
			return nil, fmt.Errorf("unknown topic %q (have %v)", n, c.Topics.Names())
		}
	}
	for _, n := range req.Formats {
		if !c.Formats.Has(n) {
			return nil, fmt.Errorf("unknown format %q (have %v)", n, c.Formats.Names())
		}
	}
	for _, n := range req.Audiences {
		if !c.Audiences.Has(n) {
			return nil, fmt.Errorf("unknown audience %q (have %v)", n, c.Audiences.Names())
		}
	}
	pick := func(requested []string, name string) bool {
		return len(requested) == 0 || slices.Contains(requested, name)
	}
	allows := func(list []string, name string) bool {
		return len(list) == 0 || slices.Contains(list, name)
	}
	all, hasAll := c.Topics.Get(AllTopics)

	var out []Selection
	for _, f := range c.Formats {
		if !pick(req.Formats, f.Name) {
			continue
		}
		format := f.Value
		if format.IsWeb() {
			for _, a := range c.Audiences {
				if !pick(req.Audiences, a.Name) || !allows(format.Audiences, a.Name) {
					continue
				}
				everything := hasAll && allows(format.Topics, AllTopics) &&
					allows(all.Formats, f.Name) && allows(all.Audiences, a.Name)
				var topics []Topic
				for _, t := range c.Topics {
					if t.Name == AllTopics || !pick(req.Topics, t.Name) {
						continue
					}
					if everything || allows(format.Topics, t.Name) &&
						allows(t.Value.Formats, f.Name) && allows(t.Value.Audiences, a.Name) {
						topics = append(topics, t.Value)
					}
				}
				if len(topics) == 0 {
					continue
				}
				out = append(out, Selection{Config: c, Topic: topics[0], Topics: topics, Format: format, Audience: a.Value})
			}
			continue
		}
		for _, t := range c.Topics {
			if t.Name == AllTopics || !pick(req.Topics, t.Name) || !allows(format.Topics, t.Name) || !allows(t.Value.Formats, f.Name) {
				continue
			}
			for _, a := range c.Audiences {
				if !pick(req.Audiences, a.Name) || !allows(format.Audiences, a.Name) || !allows(t.Value.Audiences, a.Name) {
					continue
				}
				out = append(out, Selection{Config: c, Topic: t.Value, Format: format, Audience: a.Value})
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the selection matches no topic × format × audience combination")
	}
	return out, nil
}

// AudiencesOf lists the audiences a topic takes part in.
func (c *Config) AudiencesOf(topic string) []string {
	t, ok := c.Topics.Get(topic)
	if !ok || len(t.Audiences) == 0 {
		return c.Audiences.Names()
	}
	var out []string
	for _, a := range c.Audiences {
		if slices.Contains(t.Audiences, a.Name) {
			out = append(out, a.Name)
		}
	}
	return out
}
