package web

import (
	"errors"
	"net/http"
	"net/url"
	"os"
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

// writeImage puts an image file into the project, for the image editor and
// the watch to find.
func writeImage(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSystemOpenCommand(t *testing.T) {
	for goos, want := range map[string]string{
		"windows": "explorer {file}",
		"darwin":  "open {file}",
		"linux":   "xdg-open {file}",
	} {
		if got := systemOpenCommand(goos); got != want {
			t.Errorf("systemOpenCommand(%q) = %q, want %q", goos, got, want)
		}
	}
	if got := bareServer(t).cfg.Editor.Image; got != defaultImageEditorCommand() {
		t.Errorf("a fresh installation's image editor is %q, want the system's %q", got, defaultImageEditorCommand())
	}
}

func TestOpenImageEditorStartsTheConfiguredCommand(t *testing.T) {
	srv, root := testServer(t)
	writeImage(t, root, "images/pic.png", "png")
	srv.cfg.Editor.Image = "gimp {file}"
	ran := stubEditor(t, nil)
	rec := post(t, srv, "/editor/image", url.Values{"path": {"images/pic.png"}})
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body)
	}
	if want := []string{"gimp", filepath.Join(root, "images", "pic.png")}; !slices.Equal(*ran, want) {
		t.Errorf("ran %q, want %q", *ran, want)
	}
}

// The route opens images of the project and nothing else: not a page, not
// a file outside the project, not an image that is not there.
func TestOpenImageEditorRefusesWhatIsNoImageOfTheProject(t *testing.T) {
	srv, root := testServer(t)
	writeImage(t, root, "images/pic.png", "png")
	ran := stubEditor(t, nil)
	for path, want := range map[string]string{
		"index.qmd":          "not an image",
		"../outside.png":     "invalid path",
		"images/missing.png": "is not in the project",
	} {
		body := post(t, srv, "/editor/image", url.Values{"path": {path}}).Body.String()
		if !strings.Contains(body, want) || len(*ran) != 0 {
			t.Errorf("%s: ran %q, body %s", path, *ran, body)
		}
	}

	stubEditor(t, errors.New(`exec: "gimp": executable file not found in $PATH`))
	body := post(t, srv, "/editor/image", url.Values{"path": {"images/pic.png"}}).Body.String()
	if !strings.Contains(body, "Image editor: exec") {
		t.Errorf("the failure to start is not reported:\n%s", body)
	}
}

func TestEditorConfigPageSavesTheImageCommand(t *testing.T) {
	srv, _ := configTestServer(t)
	body := get(t, srv, "/config/editor").Body.String()
	if !strings.Contains(body, `id="image-editor-command" value="`+defaultImageEditorCommand()+`"`) {
		t.Errorf("editor page does not show the default image command:\n%s", body)
	}
	if !strings.Contains(body, `data-target="image-editor-command" data-command="gimp {file}"`) {
		t.Errorf("editor page does not offer the image presets:\n%s", body)
	}

	body = post(t, srv, "/config/editor", url.Values{"image": {"krita {file}"}}).Body.String()
	if !strings.Contains(body, "Saved.") {
		t.Fatalf("not saved:\n%s", body)
	}
	if srv.cfg.Editor.Command != defaultEditorCommand {
		t.Errorf("saving the image command changed the editor command to %q", srv.cfg.Editor.Command)
	}
	again, err := newServer(srv.configFile)
	if err != nil {
		t.Fatal(err)
	}
	if again.cfg.Editor.Image != "krita {file}" {
		t.Errorf("restored image command = %q", again.cfg.Editor.Image)
	}

	body = post(t, srv, "/config/editor", url.Values{"image": {"krita"}}).Body.String()
	if !strings.Contains(body, "Put {file} where the image goes.") || srv.cfg.Editor.Image != "krita {file}" {
		t.Errorf("an image command without {file} was stored: %q\n%s", srv.cfg.Editor.Image, body)
	}
}

// An image saved on disk is announced to the browser, which fetches the
// preview's images again; the tree is left alone, since no page changed.
func TestWatchAnnouncesChangedImages(t *testing.T) {
	srv, root := testServer(t)
	writeImage(t, root, "images/pic.png", "png")
	rec := get(t, srv, "/watch")
	if rec.Code != http.StatusNoContent || rec.Header().Get("HX-Trigger") != "" {
		t.Fatalf("first watch: status %d, trigger %q; want 204 and none", rec.Code, rec.Header().Get("HX-Trigger"))
	}

	writeImage(t, root, "images/pic.png", "edited png")
	rec = get(t, srv, "/watch")
	if rec.Code != http.StatusNoContent || rec.Header().Get("HX-Trigger") != mediaChangedEvent {
		t.Errorf("after the edit: status %d, trigger %q; want 204 and %q", rec.Code, rec.Header().Get("HX-Trigger"), mediaChangedEvent)
	}
	if rec := get(t, srv, "/watch"); rec.Header().Get("HX-Trigger") != "" {
		t.Errorf("the change is announced again: %q", rec.Header().Get("HX-Trigger"))
	}

	// Copies a render writes into an output folder are not the project's
	// images.
	writeImage(t, root, "_book/images/pic.png", "copy")
	if rec := get(t, srv, "/watch"); rec.Header().Get("HX-Trigger") != "" {
		t.Errorf("an image in an output folder was announced")
	}
}
