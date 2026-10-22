package web

// external.go — opening the project in an editor outside the app.
//
// The app's own editor is for writing pages; everything else a project
// holds -- the _quarto.yml files, the filters, the stylesheets -- is better
// worked on in the editor the user already has. The top bar's Editor button
// hands the project to it, and with it the page open in the app, so the
// other editor starts where the user is.
//
// Which editor, and how it is told what to open, is one command line in the
// settings. The placeholders {root} and {file} stand for the project's
// directory and the open page's file; an argument naming {file} is left out
// when no page is open, so the same command opens the bare project then.
//
// Images get an editor of their own. A double-click on an image in the
// preview opens the image's file in it, and /watch notices when the file
// changes on disk, so the preview shows what the image editor saved
// without a reload.

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"unicode"
)

// defaultEditorCommand opens VS Code on the project and the open page in
// it. `code` is what VS Code puts on the PATH; VSCodium's is `codium`, and
// takes the same arguments.
const defaultEditorCommand = "code {root} --goto {file}"

// editorConfig is the external editors' settings: the command line that
// opens the project, and the one that opens an image.
type editorConfig struct {
	Command string `json:"command"`
	Image   string `json:"image"`
}

// defaultImageEditorCommand opens an image the way the system opens a file
// that is double-clicked, in whatever program is set up for the type.
// Windows' explorer does that when it is handed a file, and does it for
// qm under WSL too, where there is rarely a Linux desktop to ask.
func defaultImageEditorCommand() string {
	if runtime.GOOS == "linux" && inWSL() {
		return "explorer.exe {file}"
	}
	return systemOpenCommand(runtime.GOOS)
}

func systemOpenCommand(goos string) string {
	switch goos {
	case "windows":
		return "explorer {file}"
	case "darwin":
		return "open {file}"
	default:
		return "xdg-open {file}"
	}
}

// imageEditorPresets are the command lines the config page offers for the
// image editor. The first is the system's own, for the system qm runs on.
func imageEditorPresets() []editorPreset {
	return []editorPreset{
		{Name: "System default", Command: defaultImageEditorCommand()},
		{Name: "GIMP", Command: "gimp {file}"},
		{Name: "Krita", Command: "krita {file}"},
		{Name: "Inkscape", Command: "inkscape {file}"},
		{Name: "Paint (Windows)", Command: "mspaint {file}"},
	}
}

// editorPresets are the command lines the config page offers to fill in.
// Each opens the project folder and the page in it; they differ in the
// program's name and in how it is told to show a file.
var editorPresets = []editorPreset{
	{Name: "VS Code", Command: "code {root} --goto {file}"},
	{Name: "VSCodium", Command: "codium {root} --goto {file}"},
	{Name: "Cursor", Command: "cursor {root} --goto {file}"},
	{Name: "Zed", Command: "zed {root} {file}"},
	{Name: "Sublime Text", Command: "subl {root} {file}"},
}

type editorPreset struct {
	Name    string
	Command string
}

// editorPageView is the page data of /config/editor. The page has a form
// for each of the two commands, and each form's message is shown with it.
type editorPageView struct {
	Command      string
	Presets      []editorPreset
	Error        string
	Message      string
	Image        string
	ImagePresets []editorPreset
	ImageError   string
	ImageMessage string
}

// splitCommand breaks a command line into its arguments. Whitespace
// separates them; single or double quotes keep an argument with spaces in
// it together, the way a shell would, so that a program under "Program
// Files" can be named. Nothing else of a shell's is imitated: the command
// is run directly, never through one.
func splitCommand(line string) ([]string, error) {
	var args []string
	var cur strings.Builder
	var quote rune
	inArg := false
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			inArg = true
		case unicode.IsSpace(r):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unmatched %c in the editor command", quote)
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

// editorArgs turns the configured command line into the program and the
// arguments to run it with. The placeholders are replaced inside each
// argument after the split, so a path with spaces in it stays one argument.
// An argument naming {file} goes when there is no file, and so does an
// option directly before it that the file was the value of -- "--goto
// {file}" is dropped whole rather than leaving a --goto that would take the
// next argument for its value.
func editorArgs(command, root, file string) (string, []string, error) {
	parts, err := splitCommand(command)
	if err != nil {
		return "", nil, err
	}
	if len(parts) == 0 {
		return "", nil, errors.New("no editor command configured")
	}
	var out []string
	for i, p := range parts {
		if file == "" && strings.Contains(p, "{file}") {
			if i > 0 && len(out) > 0 && strings.HasPrefix(parts[i-1], "-") && !strings.Contains(parts[i-1], "{") {
				out = out[:len(out)-1]
			}
			continue
		}
		p = strings.ReplaceAll(p, "{root}", root)
		p = strings.ReplaceAll(p, "{file}", file)
		out = append(out, p)
	}
	if len(out) == 0 {
		return "", nil, errors.New("no editor command configured")
	}
	return out[0], out[1:], nil
}

// startEditor is how the editor is started; tests replace it so that no
// test starts a real one.
var startEditor = func(name string, args []string, dir string) error {
	if runtime.GOOS == "linux" && inWSL() {
		name, args = wslCommand(name, args)
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return err
	}
	// The editor outlives the request, and may outlive the app. Waiting
	// on it in the background only reaps it when it does exit.
	go cmd.Wait()
	return nil
}

// openEditorHandler opens the project, and the page named by the request if
// there is one, in the configured editor. What it answers is what the top
// bar shows beside the button: nothing when the editor started, the reason
// when it did not.
func (s *server) openEditorHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if s.root == "" {
		fmt.Fprint(w, `<span class="error-inline">Open a project first.</span>`)
		return
	}
	file := ""
	if rel := r.PostFormValue("path"); rel != "" {
		abs, err := s.resolvePath(rel)
		if err != nil {
			writeEditorError(w, err)
			return
		}
		file = abs
	}
	name, args, err := editorArgs(s.cfg.Editor.Command, s.root, file)
	if err == nil {
		err = startEditor(name, args, s.root)
	}
	if err != nil {
		writeEditorError(w, err)
	}
}

func writeEditorError(w http.ResponseWriter, err error) {
	writeError(w, "Editor", err)
}

func writeError(w http.ResponseWriter, what string, err error) {
	fmt.Fprintf(w, `<span class="error-inline" title="%s">%s: %s</span>`,
		html.EscapeString(err.Error()), what, html.EscapeString(err.Error()))
}

// openImageEditorHandler opens an image of the project in the configured
// image editor. The path is the image's path relative to the project root,
// the one the preview fetched the image by; like /media, the route takes
// image files only, so it cannot be made to open anything else of the
// project. Its answer goes where the Editor button's does.
func (s *server) openImageEditorHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	rel := r.PostFormValue("path")
	abs, err := s.resolvePath(rel)
	if err == nil && !mediaExts[strings.ToLower(path.Ext(rel))] {
		err = errors.New("not an image")
	}
	if err == nil {
		if _, statErr := os.Stat(abs); statErr != nil {
			err = fmt.Errorf("%s is not in the project", rel)
		}
	}
	var name string
	var args []string
	if err == nil {
		name, args, err = editorArgs(s.cfg.Editor.Image, s.root, abs)
	}
	if err == nil {
		err = startEditor(name, args, s.root)
	}
	if err != nil {
		writeError(w, "Image editor", err)
	}
}

func (s *server) editorConfigPage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.render(w, "editor-page", s.editorPageView())
}

func (s *server) editorPageView() editorPageView {
	return editorPageView{
		Command:      s.cfg.Editor.Command,
		Presets:      editorPresets,
		Image:        s.cfg.Editor.Image,
		ImagePresets: imageEditorPresets(),
	}
}

// saveEditorHandler stores one of the editor commands: the form for the
// project's editor posts "command", the one for images "image". A command
// that does not split is refused rather than stored: it could never be
// run. So is an image command without {file}, which would start the
// editor without the image it was asked to open.
func (s *server) saveEditorHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	view := s.editorPageView()
	_, isImage := r.PostForm["image"]
	field, setting, fail, done := &view.Command, &s.cfg.Editor.Command, &view.Error, &view.Message
	if isImage {
		field, setting, fail, done = &view.Image, &s.cfg.Editor.Image, &view.ImageError, &view.ImageMessage
	}
	command := strings.TrimSpace(r.PostFormValue("command"))
	if isImage {
		command = strings.TrimSpace(r.PostFormValue("image"))
	}
	*field = command
	parts, err := splitCommand(command)
	switch {
	case err != nil:
		*fail = err.Error()
	case len(parts) == 0:
		*fail = "Name the program to run."
	case isImage && !strings.Contains(command, "{file}"):
		*fail = "Put {file} where the image goes."
	default:
		prev := *setting
		*setting = command
		if err := s.saveConfig(); err != nil {
			*setting = prev
			*fail = "Could not save: " + err.Error()
		} else {
			*done = "Saved."
		}
	}
	s.render(w, "editor-page", view)
}
