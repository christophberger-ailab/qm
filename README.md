# qm — build and manage a Markdown documentation tree

![`qm web`](./docs/images/qm_web.png)

`qm` turns a tree of Markdown pages (`.qmd` or `.md`, in the dialect Quarto
projects use) into a website, Word documents and PDFs. It needs neither Quarto
nor Pandoc: the Markdown is parsed, filtered and written by `qm` itself.

- **One tree, many outputs.** The folder structure and the `order:` front
  matter of the pages decide the order and the nesting of every output. A
  website, a DOCX and a PDF all come from the same pages, with the headings
  demoted to the level the folder depth implies.
- **A three-axis render matrix.** A render is addressed by *topic* (what),
  *format* (how) and *audience* (for whom). One file, `qm.yml`, declares them;
  `qm render` builds any part of the matrix, and `qm web` does the same from a
  panel.
- **Conditional content.** A block or span can be marked for a format
  (`explanation`) or an audience (`pol`); a render keeps only what the
  selection shows.
- **Editing helpers.** Chapter insert/move/remove that keep the `order:` front
  matter consistent, a linter for unclosed block fences, and a local web UI
  for reordering pages by drag and drop, editing them, copyediting them with
  an LLM, and rendering.
- **Migration.** `qm migrate` writes `qm.yml` from an older Quarto setup
  (`_quarto.yml` and its profiles).

## Install

Pre-built binaries for Linux and Windows (amd64 and arm64) are attached to
each [release](../../releases). Download, unpack, put `qm` on your `PATH`.

To build from source, with Go 1.27 or newer:

```sh
go install github.com/christophberger-ailab/qm@latest
```

or, from a clone:

```sh
go build -o qm .
```

The binary is self-contained: the web UI's templates and assets, the website's
stylesheet and script, and the PDF's default fonts are embedded. No external
tool is needed. Optionally, `trash` is preferred by the deleting commands over
permanent removal.

## Usage

```
qm <object> <subcommand> [flags] [args]
```

Global flags apply to every command:

| flag | env | default | meaning |
| --- | --- | --- | --- |
| `--project`, `-p` | `QM_PROJECT` | `.` | path to the document tree |

### Commands

| command | what it does |
| --- | --- |
| `qm render [<topic>…]` | render the project's topic × format × audience matrix |
| `qm flatten <topic>` | write the Markdown a document render of one topic parses, to `_build/<topic>.md`, without rendering |
| `qm migrate` | write `qm.yml` from the project's old `_quarto*.yml` files |
| `qm lint [<topic>]` | check the `.qmd` sources (currently: unclosed `:::` fences) |
| `qm chapters insert <folder>` | give a new page its `order:`, renumbering its siblings |
| `qm chapters move <folder> <old> <new>` | move a chapter to another order position |
| `qm chapters remove <folder> <order>` | remove a chapter, after confirmation |
| `qm web [<path>]` | serve the local sorting, editing and rendering UI |

Every command takes `--help`.

#### `qm render`

```sh
qm render                              # every combination the project declares
qm render calltaker dispatcher         # those two topics, all their formats
qm render --format handout             # handouts only
qm render --audience pol --clean       # the pol variants, from scratch
qm render --dry-run                    # print what would be written, write nothing
```

Positional arguments name topics and are added to `--topic`. `--topic`,
`--format` and `--audience` take comma-separated lists; a name `qm.yml` does not
define is an error. Without them every topic, format and audience the project
offers is rendered, cut down to what each topic and format declares (see
[`qm.yml`](#qmyml)).

A **website** format covers all selected topics in one site, one site per
selected audience. A **document** format (DOCX, PDF) is written per topic and
audience. A failing combination does not stop the others; the command reports
how many failed and exits non-zero. `--clean` empties each selected format's
output directory once, before the first render; it refuses a directory that is
not below the output directory. `--dry-run` prints the files that would be
written.

`qm flatten <topic>` writes `_build/<topic>.md`: the topic's pages in folder
order, with demoted headings, in-book links turned into anchors and shortcodes
resolved. It is the text a document render parses, so it is the way to look at
what a render works from. Conditional content is still in it; a render removes
what the selected format and audience do not show.

#### `qm web`

```sh
qm web                    # serve the project at --project on localhost:8199,
                          # or the next free port up if that one is taken
qm web ../handbook        # serve that project instead
qm web --addr :9000       # listen elsewhere
```

The UI shows the project's page tree, reorders pages by drag and drop (which
rewrites the `order:` front matter and shifts the moved page's headings),
creates, edits and deletes pages with a live Markdown preview, searches the
whole project, and runs renders in the background from the same flow
`qm render` uses. The render selection and the page last open are remembered
per project in the settings file (below).

A page marked `draft: true` in its front matter is greyed out in the tree: it
is there, but it is not part of what a render publishes yet. A page written for
one audience — an `_FW` or `_POL` suffix on its name or on a folder above it —
is previewed inside that audience's `::: fw` / `::: pol` div, the way the
flattener wraps it, so the custom preview stylesheet tints and marks the whole
page just as it marks such a block written into the page by hand.

Beside the editor sit the Markdown preview and a copyedit pane. The *Preview*
and *Copyedit* buttons show and hide each on its own; with both shown they
stand side by side, a pane's head dragged onto the other swaps them, and every
divider can be dragged to resize. The copyedit pane lists the editing tasks configured under *Config → Copyedit:
Editing tasks* — each one a prompt with a title, "Passive voice", "Shorten
sentences". Clicking one sends the text the editor holds, unsaved edits and
all, to the model selected in the pane's dropdown, and answers with a list of
suggestions: what to change, what to put there instead, and why. Each
suggestion's passage is highlighted in the editor, and clicking the suggestion
scrolls to it; a Back button returns to the task list and takes the highlights
off the text.

Several tasks can be ticked and run together, with a *Select all* box above
the list for the whole set or none of it. The ticks are a setting, not
something the browser holds, so a selection stays as you move from page to
page and across restarts. By default the ticked tasks go to the model in one
call, which sends the page once instead of once per task — the page is
the bulk of what a run costs — and the findings come back grouped by task,
with a chip per task to narrow the list. Only the tasks you tick are ever sent.

*Config → Copyedit: Editing tasks* can switch that to one call per ticked
task instead. The page sits in the cached part of a request, so from the
second call on a provider that caches serves it rather than charging for it
again; what the two ways really differ in is the answer, since a model given
one task at a time attends to it fully where several in one call are answered
in one list. Which reads better depends on the model and the tasks, so the
setting is there to be tried both ways — each list of suggestions says which
way produced it.

Each suggestion carries two buttons. **Apply** writes the replacement into the
page at the highlight, so a passage that moved under an edit made since the run
is still the one replaced; it is written through the editor like a typed
change, so the autosave takes it to disk, the preview follows, and the editor's
own undo takes it back. **Done** is for a suggestion carried out by hand: it
takes the entry out of use and the highlight off the text. Suggestions with no
replacement, and those whose passage could not be found, offer Done alone.

The models are configured under *Config → Copyedit: API connections*: any
number of them, each naming how the model is reached. Two of the three kinds
are endpoints — Anthropic (`<base URL>/messages`) or OpenAI-compatible
(`<base URL>/chat/completions`, which is what OpenAI, Ollama, LM Studio and
OpenRouter all speak) — and take a base URL, the model, and the key. A local
model usually needs no key.

The third is **GitHub Copilot**, which is not an endpoint at all: qm runs the
GitHub Copilot CLI as a child process and talks to it over its own protocol.
It must be installed and on your `PATH` (or named by `COPILOT_CLI_PATH`) and
signed in — running `copilot` once is enough to check. A connection of this
kind is therefore a name and a model and nothing else: no base URL, since the
CLI knows where GitHub is, and no key, since a run is made as whoever the CLI
is signed in as. A GitHub token in the key field is used in that user's place.
The model is one the CLI offers, `gpt-5.4` or `claude-sonnet-5`; `copilot
--help` lists them. Copilot is an agent that reads files and edits
repositories, and a copyedit run wants none of that, so the session qm opens
has no tools at all, a system message that replaces the CLI's coding persona
with the copyediting instruction, and an empty scratch directory to sit in: it
can read the page it is sent and nothing else on the machine. The price is the
process — a second or so of startup per call, which in per-task mode is per
task.

The tasks and the connections are stored in the settings file (below),
readable by its owner alone since it holds the keys, and a stored key is never
sent back to the browser.

The Open field shows the last element of the project's path and offers the ten
projects opened before, listed by their full paths and remembered in the
settings file; a full path can still be typed or pasted into it. The Git button beside Render
opens a panel that lists the changes since the last commit, stages and unstages
them file by file or all at once, commits them, and pushes the branch. Clicking
a path shows that file's diff, coloured line by line, with a button that opens
the file in the editor. The panel covers the whole repository, so a project
kept in a subdirectory of a larger one is managed just the same. It runs the
`git` command, so the repository's hooks, credential helpers and config apply
as they do in a terminal.

The Editor button opens the project, and the page open in the app, in an
external editor — for `qm.yml`, stylesheets, and whatever else
the app does not edit. *Config → External editor* sets the command line it
runs, with `{root}` for the project directory and `{file}` for the page;
the default is VS Code's `code {root} --goto {file}`, and the page offers
ready-made lines for VSCodium and a few others. With no page open, the
`{file}` argument and an option right before it are left out.

Double-clicking an image in the preview opens the image's file in an image
editor, which the same config page sets: by default the program the system
opens the file with, or GIMP, Krita, Inkscape, or any other command with
`{file}` for the image. When the editor saves, the preview shows the new
version within a couple of seconds, without a reload.

### Settings

Everything the UI remembers between runs lives in one file,
`<user config dir>/qm/config.cue`: the projects opened before, the render
selection and last open page per project, which preview stylesheet is active,
the copyediting setup, and the external editors' command lines. The stylesheets themselves stay beside it in
`custom-css/`, being CSS rather than settings.

The file is CUE because it carries its own schema. The definitions are written
into it above the settings, so the file says what belongs in it and in what
shape, and every read is checked against them:

```cue
#Connection: {
    id:   string & !=""
    name: string & !=""

    // How the model is reached. "anthropic" posts to <baseURL>/messages
    // and "openai" to <baseURL>/chat/completions; "copilot" is not an
    // endpoint at all but the GitHub Copilot CLI, run as a child
    // process, which must be installed and signed in.
    kind: "anthropic" | "openai" | "copilot"
    ...
}
```

A misspelled field, a `kind` that is none of the three, a base URL that is not
a URL — each is reported with the line it is on, and the app stops rather than
starting on settings it cannot make sense of. A field left out is not an
error: lists and maps left out are empty, scalars carry the defaults the
schema names, so a file can be written by hand in part. The file is rewritten
whenever a setting changes in the UI, schema and all — values you edited by
hand are read back, comments you left among the settings are not.

The file is created with the defaults on first run. Settings from the JSON
files qm used to keep (`render.json`, `recent.json`, `copyedit.json`, and the
`custom-css/.active` marker) are carried over into it once and those files are
renamed `*.migrated`; they are no longer read and can be deleted.

## qm.yml

One file in the project root, `qm.yml`, configures everything: the output
formats, the audiences, the topics. Unknown keys are errors. Maps keep the
order they are written in, and that order is the order of the render matrix
and of the web UI's panel.

Top level keys:

| key | meaning |
| --- | --- |
| `title` | the site title and, for documents, the default document title |
| `lang` | the document language (`de`, `en-US`, …); sets the proofing language and the contents heading |
| `output` | the directory renders are written below; default `_output` |
| `vars` | variables for `{{< var name >}}` and `{name}` placeholders |
| `formats` | how a render is produced |
| `audiences` | for whom |
| `topics` | what: one content folder each |

A **format** has a `type`: `html` (a website), `docx`, `pdf`, or a list
`[docx, pdf]` that writes both from the same document. `html` cannot be
combined with another type. If `type` is left out, it is guessed from the
name (`docx`, `word`, `handout` and `handbook` mean DOCX, `pdf` means PDF,
anything else HTML). More keys: `show` (the conditional classes the format
keeps), `toc` (a table of contents in a document), `template` (a reference
DOCX), `dir` (output directory below `output`; default the format's name),
`file` (a document's file name without extension; default `{topic}`, plus
`-{audience}` when the project has more than one audience), `topics` and
`audiences` (restrict the matrix from the format's side) and `vars`.

An **audience** has an optional `title` (the label in the web UI), `show` (the
conditional classes it keeps; its own name is always one of them) and `vars`.

A **topic** has `folder` (the content folder, project-relative; default the
topic's name), `title` (overrides the title of the folder's index page),
`cover` (a PNG put in front of a document), `formats` and `audiences`
(restrict the matrix from the topic's side) and `vars`. Where a topic and a
format both restrict an axis, both have to agree.

A complete example:

```yaml
title: Handbuch Leitstelle
lang: de
output: _output
vars:
  org: Leitstelle Nord
  suffix: ""
formats:
  web:
    type: html
  handout:
    type: [docx, pdf]
    toc: true
    template: assets/templates/Template.docx
    show: [explanation]
    topics: [calltaker, dispatcher]
  handbook:
    type: docx
    toc: true
    template: assets/templates/Template{suffix}.docx
    show: [explanation, tutorial]
    dir: handbook
    file: "{topic}{suffix}"
audiences:
  std:
    title: Standard
  pol:
    title: Polizei
    show: [pol]
    vars: {suffix: _POL}
  fw:
    title: Feuerwehr
    show: [fw]
    vars: {suffix: _FW}
topics:
  calltaker:
    cover: "assets/covers/calltaker{suffix}.png"
  dispatcher:
    cover: "assets/covers/dispatcher{suffix}.png"
  sysadmin:
    folder: systemadministration
    title: Systemadministration
    audiences: [std]
```

(`suffix` is an example variable: it means something only because `template`,
`file` and `cover` use it. The project-level default keeps `{suffix}` defined
for the audience `std`, which sets none.)

A minimal project needs no `qm.yml` at all. Without one, every top-level
content folder (one holding a `.qmd` or `.md` file, and not starting with `_`
or `.`) is a topic, the only format is `web` (HTML), and there is one
audience, `all`. A `qm.yml` states only what differs:

```yaml
title: Handbook
lang: en
```

`{name}` placeholders in `dir`, `file`, `cover` and `template` resolve against
the variables: the project's `vars`, then the topic's, the audience's and the
format's (later ones win), plus the built-ins `topic`, `format` and
`audience`. A placeholder without a variable is an error. The same variables
serve `{{< var name >}}` in the content, together with the keys of
`_variables.yml` in the project root (nested keys are written with dots,
`metadata.version`); `vars` win over the file.

### Conditional content

A div or span carrying a class that a format or an audience lists under
`show:` — or the name of an audience — is *conditional*:

```markdown
::: explanation
Shown in the handbook, which lists `explanation` under `show:`.
:::

::: pol
Only for the `pol` audience.
:::

Fire engines [(Feuerwehr only)]{.fw} arrive first.
```

A render keeps a conditional div or span only if the selection shows it.
Format classes and audience classes are checked separately, and a block that
carries both kinds must pass both. Classes that no format or audience lists
are ordinary styling classes and do not matter. A div that is not kept is
removed with its content; one that is kept stays as a wrapper for its content.

Quarto's `.content-visible` and `.content-hidden` divs with `when-format` or
`unless-format` also work; they compare against the format's types (`html`,
`docx`, `pdf`) and its name. Conditions on profiles or metadata are not
supported, and such content is kept.

A `::: slide` div and a `.notes` div are always dropped. PPTX slide decks are
not supported for now.

The project's Lua filters no longer run. What an audience filter did there is
done by `show:`.

### Heading demotion

Folder structure and `order:` drive every output; there is no distinction
between a "book" and a "website". Pages that lie directly in a topic's folder,
and the `index.qmd` of a subfolder (which sorts among its *parent's* pages),
use their headings as written. Every deeper folder level adds one heading
level. A page that writes `##` two levels below the topic folder therefore
ends up as `####`. A page whose front-matter `title:` is not spelled out by a
heading at its top level gets a generated title heading, and its own headings
stay below it.

`qm flatten <topic>` shows the result for a document.

### The website

An HTML format builds a site with a layout like Quarto's:

- a top navbar with one entry per topic, and the site title linking to the
  home page, which lists the topics;
- a left sidebar with the current topic's page tree;
- a right "On this page" table of contents with scroll-spy;
- previous/next links at the foot of a page;
- a responsive layout: the contents list is hidden at 75em and narrower, the
  sidebar becomes off-canvas at 50em and narrower;
- all text sizes and the 42em text column are in `em`, so the page scales with
  the reader's font size; dark mode follows the system setting; print styles;
- all links are relative, so the site works from `file://` and below any URL
  prefix.

Callouts, tables, footnotes, definition lists and images are drawn by the
site's own stylesheet. Links between pages of a topic (`/dispatcher/a/`,
`/dispatcher/a/index.qmd`, …) are rewritten to the generated pages. With
several audiences, a site goes to `<output>/<format>-<audience>` unless the
format's `dir` contains a placeholder.

### Documents: DOCX and PDF

A document format flattens one topic into one Markdown text, resolves its
shortcodes, parses it, removes the conditional content the selection does not
show, and writes it. The shortcodes `include`, `var` and `pagebreak` are
resolved by `qm`:

- `{{< include /path/file.md >}}` takes a project-absolute path, nested up to
  eight deep;
- `{{< var name >}}` resolves as described above;
- `{{< pagebreak >}}` starts a new page.

Any other shortcode is reported and left as written.

Documents are written to `<output>/<dir>/<file>.docx` and `.pdf`, which by
default is `_output/<format>/<topic>[-<audience>]`.

**DOCX** is written in Go. A `template:` names a reference DOCX; its styles,
page setup, headers, footers and numbering are used, and its body is
discarded. Without one, a blank document with Word's default styles is used.
`toc: true` adds a Word contents field, which Word fills when fields are
updated.

**PDF** is laid out in Go (A4, embedded fonts, headings, lists, tables,
images, links, bookmarks, page numbers, a linked contents list). It is fed
from the same parsed document as the DOCX. The PDF sets its text in the Go
fonts, unless the project root has a
[`_brand.yml`](https://posit-dev.github.io/brand-yml/) whose `typography`
names fonts with local files. Then body text uses `base` (regular, bold,
italic and bold italic, each the closest weight and style the family
offers), headings use `headings` (default weight bold), and code uses
`monospace`. Only TrueType files (`.ttf`) can be embedded; `.otf`, WOFF files
and fonts from Google, Bunny or the system are skipped with a note in the
render log, and their text keeps the Go fonts. Font sizes, colors and line
heights from the brand file do not apply to the PDF.

```yaml
typography:
  fonts:
    - family: Source Sans 3
      source: file
      files:
        - path: fonts/SourceSans3-Regular.ttf
        - path: fonts/SourceSans3-Bold.ttf
          weight: bold
        - path: fonts/SourceSans3-Italic.ttf
          style: italic
  base: Source Sans 3
  headings:
    family: Source Sans 3
    weight: semi-bold
```

**Covers.** A topic's `cover:` is a project-relative PNG, and may use
placeholders. It is put in front of the DOCX and the PDF. Both covers keep
the image's aspect ratio and are anchored at the bottom-right corner of the
page, where the brand graphic ends. The PDF cover (A4) fills the page and
crops any overflow at the top or left. The DOCX cover uses the document's page
size, shrinks to fit, and never crops. It sits in a dedicated zero-margin
section, and the body's original section properties, styles, headers and
footers are preserved. No header or footer is added to either cover. A
missing or invalid cover fails the render before anything is written.

The PDF has its own layout; it does not reproduce the Word template. Library
research and rationale: [docs/native-rendering.md](docs/native-rendering.md).

### Migrating an existing project

```sh
qm migrate --dry-run     # print the qm.yml that would be written
qm migrate               # write it
qm migrate --force       # overwrite an existing qm.yml
```

`qm migrate` reads `_quarto.yml` and the profile files
`_quarto-topic-<t>.yml`, `_quarto-format-<f>.yml` and
`_quarto-audience-<a>.yml` and writes the equivalent `qm.yml`: `qm:` keys such
as `folder`, `formats`, `audiences` and `cover`, the `make:` flags that are
true, the `_quarto-vars`, and the output directories. It refuses to
overwrite an existing `qm.yml` without `--force`, checks that the result loads,
and prints a warning for everything it could not carry over: a title that
contains a variable, an output directory outside the common output directory,
a template generated into `_build/`, a slide deck format (skipped) or a PPTX
output (dropped).

The old `_quarto*.yml` files are **not read anymore** by `qm` itself. Delete
them once you no longer render with Quarto; the `project: pre-render` and
`post-render` hooks (`qm prepare`, `qm finalize`) no longer exist and must go
from `_quarto.yml` if you keep the file for other tools.

### Known differences from Quarto

- No slide decks (PPTX): `::: slide` blocks and `.notes` are dropped. A
  follow-up will bring them back.
- Mermaid diagrams (` ```{mermaid} `) stay code blocks, with a warning.
- No math typesetting.
- The site has no search, and raw HTML links in a page are not rewritten.
- The project's Lua filters do not run; use `show:` for audience gates.
- Shortcodes other than `include`, `var` and `pagebreak` are not resolved.
- The PDF does not reproduce the Word template, and the DOCX table of
  contents is filled by Word when fields are updated.

## Page conventions

- Every page carries an `order:` in its front matter; a folder is ordered by
  its `index.qmd`, which sorts among its *parent's* pages.
- Names starting with `_` or `.` are invisible.
- A `_FW` or `_POL` suffix on a file or folder name marks it for one audience.
  A variant file supersedes its plain counterpart for the matching audience,
  and the flattener wraps variant content in a `::: fw` / `::: pol` div. That
  div is conditional like any other: it is kept for an audience named `fw`
  (or `pol`) or one that lists the class under `show:`.
- Pages can be written as `.qmd` or `.md`.

## Repository layout

```
main.go                 command wiring: flags, the `chapters` object, registration
internal/cli/           the exit-status guard every command body is wrapped in
internal/config/        qm.yml: formats, audiences, topics, the render matrix, selections
internal/qmcore/        front matter, page scans, the chapters' ordering (and the old profiles, for migrate)
internal/bookmaker/     folder tree → one flat document, headings demoted by depth
internal/markdown/      the Markdown parser (goldmark plus divs, spans, attributes, callouts) and the conditional filter
internal/shortcode/     include, var, pagebreak
internal/build/         a render: flatten, parse, filter, write; brand fonts
internal/site/          the website writer
internal/docx/          the DOCX writer
internal/docrender/     PDF layout, covers, and the Markdown → PDF input
internal/project/       the editable page tree behind the web UI
internal/gitrepo/       the `git` command behind the web UI's Git panel
web/                    the local web UI: server, settings (CUE), copyediting
insert/ move/ remove/   the `qm chapters` subcommands
lint/ flatten/ render/ migrate/ web/   the leaf commands
spec.yaml, spec-*.yaml  the specification each package implements
testdata/               a miniature project and the flattener's golden files
```

`WALKTHROUGH.md` reads the whole codebase once, in dependency order, and
explains why each piece exists. Start there if you intend to change something.

## Development

```sh
go test ./...
go vet ./...
```

The specification lives in `spec.yaml` (project-wide constraints) and one
`spec-<command>.yaml` per subcommand, written in the
[acai feature-spec format](./acai-feature-spec.yaml) documented in this
repository. Code comments reference requirements by id, e.g. `BOOKS.2-1`. When
behaviour changes, change the spec with it.

Releases are cut by `.github/workflows/release.yml` when a pull request is
merged into `main`: it vets, tests, cross-compiles the four targets, and
publishes a release with checksums. Versions follow [SemVer](https://semver.org)
with the major version held at 0: label a PR `semver:minor` when it adds or
improves a feature, which bumps the minor version; a PR with only fixes
(`semver:patch` or no label) bumps the patch level. A manual run of the
workflow can pick the bump or set an exact `v0.Y.Z` version.

## Origins

`qm web` is the [quarto-sorter](https://github.com/christophberger-ailab/quarto-sorter)
tool merged into `qm`; `internal/bookmaker` is a copy of the
[quarto-bookmaker](https://github.com/christophberger-ailab/quarto-bookmaker)
internal package (MIT), which Go's `internal` rule prevents importing directly.
Keep changes to it in sync with upstream.

## License

BSD 3-Clause — see [LICENSE](./LICENSE).

The web UI bundles third-party front-end libraries, each under its own license:
CodeMirror 5 (MIT, with its license text at
`web/assets/static/codemirror/LICENSE`), marked (MIT), Sortable (MIT), and htmx
(Zero-Clause BSD).
