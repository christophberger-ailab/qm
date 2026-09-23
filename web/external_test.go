package web

import (
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"code {root} --goto {file}", []string{"code", "{root}", "--goto", "{file}"}},
		{`  "C:/Program Files/Editor/ed.exe"  {root} `, []string{"C:/Program Files/Editor/ed.exe", "{root}"}},
		{`ed 'a b'c ""`, []string{"ed", "a bc", ""}},
		{"", nil},
	} {
		got, err := splitCommand(tc.line)
		if err != nil {
			t.Errorf("splitCommand(%q): %v", tc.line, err)
			continue
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("splitCommand(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
	if _, err := splitCommand(`code "unterminated`); err == nil {
		t.Error("an unmatched quote was accepted")
	}
}

func TestEditorArgs(t *testing.T) {
	root := "/work/my book"
	file := "/work/my book/intro.qmd"
	for _, tc := range []struct {
		command, file string
		name          string
		args          []string
	}{
		// The paths go in after the split, so a space in them keeps them whole.
		{"code {root} --goto {file}", file, "code", []string{root, "--goto", file}},
		// With no page open, the option that took the file goes with it.
		{"code {root} --goto {file}", "", "code", []string{root}},
		{"zed {root} {file}", "", "zed", []string{root}},
		{"ed {root} {file}:1", file, "ed", []string{root, file + ":1"}},
		{"ed {root}", file, "ed", []string{root}},
	} {
		name, args, err := editorArgs(tc.command, root, tc.file)
		if err != nil {
			t.Errorf("editorArgs(%q, %q): %v", tc.command, tc.file, err)
			continue
		}
		if name != tc.name || !slices.Equal(args, tc.args) {
			t.Errorf("editorArgs(%q, %q) = %q %q, want %q %q", tc.command, tc.file, name, args, tc.name, tc.args)
		}
	}
	if _, _, err := editorArgs("   ", root, ""); err == nil {
		t.Error("an empty command was accepted")
	}
}

// stubEditor records what the Editor button would have started instead of
// starting it.
func stubEditor(t *testing.T, fail error) *[]string {
	t.Helper()
	var got []string
	prev := startEditor
	startEditor = func(name string, args []string, dir string) error {
		got = append([]string{name}, args...)
		return fail
	}
	t.Cleanup(func() { startEditor = prev })
	return &got
}

func TestOpenEditorStartsTheConfiguredCommand(t *testing.T) {
	srv, root := testServer(t)
	ran := stubEditor(t, nil)
	rec := post(t, srv, "/editor/open", url.Values{"path": {"intro.qmd"}})
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body)
	}
	want := []string{"code", root, "--goto", filepath.Join(root, "intro.qmd")}
	if !slices.Equal(*ran, want) {
		t.Errorf("ran %q, want %q", *ran, want)
	}

	*ran = nil
	post(t, srv, "/editor/open", url.Values{})
	if want := []string{"code", root}; !slices.Equal(*ran, want) {
		t.Errorf("with no page: ran %q, want %q", *ran, want)
	}
}

func TestOpenEditorReportsWhatWentWrong(t *testing.T) {
	srv, _ := testServer(t)
	ran := stubEditor(t, errors.New(`exec: "code": executable file not found in $PATH`))
	body := post(t, srv, "/editor/open", url.Values{"path": {"../outside.qmd"}}).Body.String()
	if !strings.Contains(body, "invalid path") || len(*ran) != 0 {
		t.Errorf("a path outside the project was not refused: ran %q, body %s", *ran, body)
	}
	body = post(t, srv, "/editor/open", url.Values{}).Body.String()
	if !strings.Contains(body, "executable file not found") {
		t.Errorf("the failure to start is not reported:\n%s", body)
	}
}

func TestEditorConfigPageSavesTheCommand(t *testing.T) {
	srv, root := configTestServer(t)
	body := get(t, srv, "/config").Body.String()
	if !strings.Contains(body, `href="/config/editor"`) {
		t.Errorf("config page does not link the editor setting:\n%s", body)
	}
	body = get(t, srv, "/config/editor").Body.String()
	if !strings.Contains(body, `value="`+defaultEditorCommand+`"`) {
		t.Errorf("editor page does not show the default command:\n%s", body)
	}

	body = post(t, srv, "/config/editor", url.Values{"command": {"codium {root} --goto {file}"}}).Body.String()
	if !strings.Contains(body, "Saved.") {
		t.Fatalf("not saved:\n%s", body)
	}
	again, err := newServer(srv.configFile)
	if err != nil {
		t.Fatal(err)
	}
	if again.cfg.Editor.Command != "codium {root} --goto {file}" {
		t.Errorf("restored command = %q", again.cfg.Editor.Command)
	}

	body = post(t, srv, "/config/editor", url.Values{"command": {`codium "{root}`}}).Body.String()
	if !strings.Contains(body, "unmatched") || srv.cfg.Editor.Command != "codium {root} --goto {file}" {
		t.Errorf("a broken command was stored: %q\n%s", srv.cfg.Editor.Command, body)
	}

	ran := stubEditor(t, nil)
	post(t, srv, "/editor/open", url.Values{})
	if want := []string{"codium", root}; !slices.Equal(*ran, want) {
		t.Errorf("ran %q, want %q", *ran, want)
	}
}

// The Editor button belongs to an open project, and /open brings it along
// with the rest of the top bar.
func TestEditorButtonComesWithTheProject(t *testing.T) {
	if body := get(t, bareServer(t), "/").Body.String(); strings.Contains(body, `id="open-external"`) {
		t.Errorf("Editor button shown with no project open")
	}
	srv, _ := testServer(t)
	if body := get(t, srv, "/").Body.String(); !strings.Contains(body, `id="open-external"`) {
		t.Errorf("Editor button missing:\n%s", body)
	}
	body := post(t, srv, "/open", url.Values{"path": {fixture(t)}}).Body.String()
	if !strings.Contains(body, `hx-swap-oob="innerHTML:#editor-panel"`) {
		t.Errorf("open does not refresh the Editor button:\n%s", body)
	}
}

// The dropdown of recent projects lists them by their full paths, while
// the field keeps showing the folder name alone.
func TestRecentDropdownShowsFullPaths(t *testing.T) {
	srv, root := testServer(t)
	body := get(t, srv, "/").Body.String()
	if !strings.Contains(body, `data-label="`+pathLabel(root)+`" title="`+root+`">`+root+`</button>`) {
		t.Errorf("dropdown entry does not show the full path %q:\n%s", root, body)
	}
	if !strings.Contains(body, `value="`+pathLabel(root)+`"`) {
		t.Errorf("field does not show the folder name:\n%s", body)
	}
}
