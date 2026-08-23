# qm — a companion CLI for Quarto documentation trees

`qm` manages a [Quarto](https://quarto.org) documentation tree and adds the
things the `quarto` command itself does not do:

- **Books out of nested folders.** A Quarto book is flat — one file per
  chapter, folder depth carries no meaning. `qm` flattens a whole content
  folder into a single document, demoting each page's headings to the level its
  folder depth implies, rewriting in-book links into anchors, and lifting the
  pages' `::: slide` blocks out into a slide deck of its own.
- **A three-axis render matrix.** A render is addressed by *topic* (what),
  *format* (how) and *audience* (for whom), each an ordinary Quarto profile.
  `qm` validates a selection, expands the matrix the project declares, and
  drives one `quarto render` per combination.
- **The output paths Quarto cannot compose.** Quarto resolves `{{< var >}}` in
  only two configuration keys. `qm` fills the gap as the project's pre- and
  post-render hooks, so a plain `quarto render --profile …` produces a
  correctly built and correctly named artefact without `qm` on the command
  line.
- **Editing helpers.** Chapter insert/move/remove that keep the `order:` front
  matter consistent, a linter for unclosed Quarto block fences, and a local web
  UI for reordering pages by drag and drop, editing them, and rendering.

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

The binary is self-contained: the web UI's templates and assets are embedded.
The only external tool it needs is `quarto` itself, on `PATH`, and — optionally
— `trash`, which the deleting commands prefer over permanent removal.

## Usage

```
qm <object> <subcommand> [flags] [args]
```

Global flags apply to every command:

| flag | env | default | meaning |
| --- | --- | --- | --- |
| `--project`, `-p` | `QM_PROJECT` | `.` | path to the Quarto document tree |

### Commands

| command | what it does |
| --- | --- |
| `qm render [<topic>…]` | render the project's topic × format × audience matrix |
| `qm flatten <topic>` | write the build documents of one topic without rendering |
| `qm prepare` | pre-render hook: validate the selection, build the topic |
| `qm finalize` | post-render hook: move the output where the profiles say |
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
qm render --dry-run                    # print the quarto invocations, run none
```

Each combination becomes one
`quarto render --profile topic-<t>,format-<f>,audience-<a> --no-clean`, run in
the project root. A failing combination does not stop the others; the command
reports how many failed and exits non-zero.

`--clean` empties each selected format's output directory once, before the
first render. It is a `qm` flag rather than Quarto's own cleaning because
several topics and audiences share one output directory, so a per-run clean
would delete what the run before it produced.

#### `qm web`

```sh
qm web                    # serve the project at --project on localhost:8199
qm web ../handbook        # serve that project instead
qm web --addr :9000       # listen elsewhere
```

The UI shows the project's page tree, reorders pages by drag and drop (which
rewrites the `order:` front matter and shifts the moved page's headings),
creates, edits and deletes pages with a live Markdown preview, searches the
whole project, and runs renders in the background from the same flow
`qm render` uses. The render selection and the page last open are remembered
per project in `<user config dir>/qm/render.json`.

## How a project is set up

`qm` expects a Quarto project laid out along three profile axes.

**One profile per axis**, named `_quarto-<axis>-<value>.yml` in the project
root:

```
_quarto-topic-calltaker.yml     what is rendered  — the content folder
_quarto-format-handout.yml      how               — project type, output dir, formats
_quarto-audience-pol.yml        for whom          — the flags the content filter reads
```

A render selects exactly one of each; two on an axis, a missing axis, or a
profile with no file are errors.

**A topic profile declares what it takes part in**, under the `qm:` key that
Quarto ignores and `qm` owns:

```yaml
# _quarto-topic-sysadmin.yml
qm:
  folder:    systemadministration    # default: the topic name
  formats:   [handout, handbook, slides]
  audiences: [std]
```

Declaring nothing means every value the project offers. A format profile may
narrow the same axes from its side; where both declare one, both have to agree.

**The book is a fixed one-element chapter list.** `_quarto.yml` holds

```yaml
book:
  chapters: [index.qmd]
```

and `index.qmd` includes `_build/book.qmd`, which the pre-render hook writes.
A profile must never set `book: chapters:` — Quarto concatenates array keys
across profiles instead of replacing them, so the book would render twice.

**The hooks do the rest**:

```yaml
# _quarto.yml
project:
  pre-render:  qm prepare
  post-render: qm finalize
```

`qm prepare` validates the active profile selection, flattens the selected
topic into `_build/book.qmd` and `_build/slides.qmd`, and copies the files a
format's `qm: copy:` map asks for. `qm finalize` applies the format's
`qm: output-file:` and `qm: output-dir-suffix:` to what Quarto produced.

Because both are wired into the project, a plain
`quarto render --profile topic-x,format-y,audience-z` — from a terminal, from
VS Code, from CI — produces exactly the same artefact as `qm render`.

### Page conventions

- Every page carries an `order:` in its front matter; a folder is ordered by
  its `index.qmd`, which sorts among its *parent's* pages.
- Names starting with `_` or `.` are invisible, as they are to Quarto.
- A `_FW` or `_POL` suffix on a file or folder name marks it for one audience.
  A variant file supersedes its plain counterpart for the matching audience,
  and the flattener wraps variant content in a `::: fw` / `::: pol` div so the
  project's content filter can select it.
- A `::: slide` div holds content for the deck rather than the book; the
  flattener moves it into `_build/slides.qmd`.

## Repository layout

```
main.go              command wiring: flags, the `chapters` object, registration
internal/cli/        the exit-status guard every command body is wrapped in
internal/qmcore/     axes, selections, profiles, the render matrix, front matter
internal/bookmaker/  folder tree → one flat document (+ the slide deck)
internal/bookrender/ the build documents and the `quarto` invocation
internal/project/    the editable page tree behind the web UI
insert/ move/ remove/  the `qm chapters` subcommands
lint/ flatten/ render/ prepare/ finalize/ web/   the leaf commands
spec.yaml, spec-*.yaml   the specification each package implements
testdata/            a miniature Quarto project and the flattener's golden files
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
publishes a `v0.0.<run-number>` release with checksums.

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
