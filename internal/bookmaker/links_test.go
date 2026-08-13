package bookmaker

import (
	"reflect"
	"testing"
)

// testTargets builds the link map for a small book tree.
func testTargets(t *testing.T) linkTargets {
	t.Helper()
	root := &Node{Rel: "book", Dir: "book", Children: []*Node{
		{Rel: "book/intro", Dir: "book/intro"},
		{Rel: "book/deep", Dir: "book/deep", Children: []*Node{
			{Rel: "book/deep/page", Dir: "book/deep/page"},
			{Rel: "book/deep/leaf"}, // standalone .qmd, not a directory
		}},
	}}
	assignAnchors(root)
	return buildLinkTargets(root)
}

func TestLinkTargetsLookup(t *testing.T) {
	targets := testTargets(t)

	tests := []struct {
		name    string
		dest    string
		baseDir string
		want    string
		ok      bool
	}{
		{name: "directory url", dest: "/book/intro/", want: "#bm-book-intro", ok: true},
		{name: "directory url without slash", dest: "/book/intro", want: "#bm-book-intro", ok: true},
		{name: "index source path", dest: "/book/intro/index.qmd", want: "#bm-book-intro", ok: true},
		{name: "rendered html", dest: "/book/deep/page/index.html", want: "#bm-book-deep-page", ok: true},
		{name: "standalone page", dest: "/book/deep/leaf.qmd", want: "#bm-book-deep-leaf", ok: true},
		{name: "standalone page html", dest: "/book/deep/leaf.html", want: "#bm-book-deep-leaf", ok: true},

		{name: "relative sibling", dest: "../page/", baseDir: "book/deep/leaf", want: "#bm-book-deep-page", ok: true},
		{name: "relative child", dest: "page/", baseDir: "book/deep", want: "#bm-book-deep-page", ok: true},

		{name: "keeps an explicit fragment", dest: "/book/intro/#details", want: "#details", ok: true},
		{name: "ignores a query string", dest: "/book/intro/?x=1", want: "#bm-book-intro", ok: true},

		{name: "outside the book", dest: "/other/page/"},
		{name: "asset", dest: "/assets/images/x.png"},
		{name: "external", dest: "https://example.com/book/intro/"},
		{name: "protocol relative", dest: "//example.com/book/intro/"},
		{name: "mailto", dest: "mailto:a@b.c"},
		{name: "bare fragment", dest: "#already"},
		{name: "empty", dest: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := targets.lookup(tc.dest, tc.baseDir)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got %q)", ok, tc.ok, got)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRewriteLinks(t *testing.T) {
	targets := testTargets(t)

	tests := []struct {
		name    string
		in      string
		want    string
		changed int
	}{
		{
			name:    "inline link",
			in:      "See [Intro](/book/intro/) for details.",
			want:    "See [Intro](#bm-book-intro) for details.",
			changed: 1,
		},
		{
			name: "image destinations are never touched",
			in:   "![Intro](/book/intro/)",
			want: "![Intro](/book/intro/)",
		},
		{
			name:    "image wrapped in a link",
			in:      "[![alt](/assets/x.png)](/book/intro/)",
			want:    "[![alt](/assets/x.png)](#bm-book-intro)",
			changed: 1,
		},
		{
			name:    "link title is preserved",
			in:      `[Intro](/book/intro/ "the intro")`,
			want:    `[Intro](#bm-book-intro "the intro")`,
			changed: 1,
		},
		{
			name:    "angle bracket destination",
			in:      "[Intro](</book/intro/>)",
			want:    "[Intro](#bm-book-intro)",
			changed: 1,
		},
		{
			name:    "several links on one line",
			in:      "[a](/book/intro/) and [b](/book/deep/page/) and [c](/elsewhere/)",
			want:    "[a](#bm-book-intro) and [b](#bm-book-deep-page) and [c](/elsewhere/)",
			changed: 2,
		},
		{
			name:    "reference definition",
			in:      "[intro]: /book/intro/",
			want:    "[intro]: #bm-book-intro",
			changed: 1,
		},
		{
			name: "links inside code blocks are left alone",
			in:   "```\n[a](/book/intro/)\n```",
			want: "```\n[a](/book/intro/)\n```",
		},
		{
			name: "media paths survive",
			in:   "![Bord](/assets/images/dispatcher_einsatzbord_POL.png)",
			want: "![Bord](/assets/images/dispatcher_einsatzbord_POL.png)",
		},
		{
			name: "span attributes are not links",
			in:   "[![Bord](/assets/x.png)]{.pol}",
			want: "[![Bord](/assets/x.png)]{.pol}",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := rewriteLinks(lines(tc.in), targets, "book")
			if want := lines(tc.want); !reflect.DeepEqual(got, want) {
				t.Errorf("got %q\nwant %q", got, want)
			}
			if changed != tc.changed {
				t.Errorf("changed = %d, want %d", changed, tc.changed)
			}
		})
	}
}

func TestIsImageLink(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"![alt](/x.png)", true},
		{"[text](/x)", false},
		{"[![alt](/x.png)](/y)", false},     // the outer link
		{"prefix ![a [b] c](/x.png)", true}, // brackets inside the alt text
		{`\![escaped](/x)`, false},          // the bang is escaped
	}
	for _, tc := range tests {
		p := lastIndexOfLinkTail(tc.line)
		if p < 0 {
			t.Fatalf("no `](` in %q", tc.line)
		}
		if got := isImageLink(tc.line, p); got != tc.want {
			t.Errorf("isImageLink(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// lastIndexOfLinkTail finds the final `](` in a line, which is the one that
// belongs to the outermost link.
func lastIndexOfLinkTail(s string) int {
	for i := len(s) - 2; i >= 0; i-- {
		if s[i] == ']' && s[i+1] == '(' {
			return i
		}
	}
	return -1
}

func TestParseLinkTail(t *testing.T) {
	tests := []struct {
		line string
		open int
		dest string
		rest string
		end  int
		ok   bool
	}{
		{line: "](/a/b)", open: 1, dest: "/a/b", end: 7, ok: true},
		{line: `](/a "t")`, open: 1, dest: "/a", rest: ` "t"`, end: 9, ok: true},
		{line: "](</a b>)", open: 1, dest: "/a b", end: 9, ok: true},
		{line: "](/a", open: 1, ok: false},
	}
	for _, tc := range tests {
		dest, rest, end, ok := parseLinkTail(tc.line, tc.open)
		if ok != tc.ok {
			t.Fatalf("%q: ok = %v, want %v", tc.line, ok, tc.ok)
		}
		if !ok {
			continue
		}
		if dest != tc.dest || rest != tc.rest || end != tc.end {
			t.Errorf("%q: got (%q, %q, %d), want (%q, %q, %d)",
				tc.line, dest, rest, end, tc.dest, tc.rest, tc.end)
		}
	}
}
