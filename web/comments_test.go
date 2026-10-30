package web

import (
	"strings"
	"testing"
)

// A passage is commented in the editor and highlighted in the preview
// (spec-web.yaml 3-8). Both happen in the browser; what a Go test can check
// is that the scripts and the stylesheet still carry the pieces that do it.
func TestCommentsInEditorAndPreview(t *testing.T) {
	for file, wants := range map[string][]string{
		"assets/static/editor.js":  {"function addComment(", "function focusComment(", "'Ctrl-Alt-M': addComment", "mountCommentRail();"},
		"assets/static/preview.js": {"function parseCommentMarker(", "function markComments(", "markComments(el);", "marked.lexer(unblockComments(src))"},
		"assets/static/app.js":     {"focusComment(Number("},
		"assets/static/app.css":    {"--comment:", ".comment-rail", ".comment-add", ".qm-comment"},
	} {
		src, err := assets.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(src), want) {
				t.Errorf("%s does not handle comments, missing %q", file, want)
			}
		}
	}
}
