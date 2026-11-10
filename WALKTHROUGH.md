# A walkthrough of the qm source

This document reads the code once, from front to back, in the order the
pieces depend on each other. It follows one `qm render` from the command line
down to the written file, then doubles back for the commands that do not take
part in a render (`chapters`, `lint`, `migrate`) and for the web UI.

Every section names the files it covers, so it can also be used as a map.

---

## 0. The facts the code is built around

qm used to drive Quarto and Pandoc; it now needs neither. What remains of that
history is a set of facts about the content, which explain most of the design:

1. **The documentation is a folder tree.** Folder structure and the `order:`
   front matter decide the order and nesting of every output, and a
   multi-level folder has to become one flat document (DOCX, PDF) or a tree of
   pages (website). That is `internal/bookmaker`, and the same tree feeds both.
2. **A render is a point in a three-axis matrix**: topic (which folder),
   format (how) and audience (for whom). All three are declared in one file,
   `qm.yml` (`internal/config`), whose maps are ordered and whose `{name}`
   placeholders resolve against variables.
3. **The content is Quarto-flavoured Markdown**: fenced divs, bracketed
   spans, attributes, callouts, shortcodes. `internal/markdown` parses it in Go
   (goldmark plus extensions), and `internal/shortcode` resolves the three
   shortcodes the content uses.
4. **Conditional content replaces the Lua filters.** A div or span whose class
   a format or audience lists under `show:` survives only when the selection
   shows it. It is one function, `config.Selection.Keep`, applied to the
   parsed tree.
5. **Every output is written from that one parsed tree**: the website by
   `internal/site`, the DOCX by `internal/docx`, the PDF by
   `internal/docrender`. `internal/build` is the glue.
6. **A command's failure must show in the exit status**, because scripts and
   CI read it. That is `internal/cli`.

The formal specification of all of this lives in `spec.yaml` and the
`spec-*.yaml` files, one per subcommand, in the [acai feature-spec
format](./acai-feature-spec.yaml). The code comments reference them by
requirement id (`SUBCOMMANDS.3`, `BOOKS.2-1`, …).

---

## 1. `main.go` — the wiring

`main.go` is short and does four things.

**It declares the flag that is not owned by a single command.**
`--project`/`-p` is qm-wide and passed to the subcommands as `*string`, which is
the shape [`github.com/christophberger/start`](https://github.com/christophberger/start)
uses — the flag is parsed by the time a command body runs. (The `--profile`
flag the Quarto hooks used is gone with them.)

**It models the `<object>` parameter.** The command pattern is
`qm <object> <subcommand> [flags]`, but `start` has no notion of an object. So
`chapters` is registered as a parent command with no `Cmd` of its own
(`main.go:59`), and `insert`, `move`, `remove` register themselves underneath
it. Leaving `Cmd` nil is deliberate: `start` then auto-generates the "missing
subcommand" usage instead of panicking.

**It registers everything else as a leaf command**: `lint`, `render`,
`flatten`, `migrate`, `web`. Each of those packages exposes a single
`Register(...)` function and keeps its flag pointers in package-level
variables — the same pattern in all command packages, so once you have read
one you have read them all.

**It sets the exit status.** `start.Up()` prints a command's error and returns
normally, leaving the status at 0. `cli.Exit()` is what turns a
failure into a non-zero exit — see the next section.

`version` is `"dev"` unless the release workflow stamps it via
`-ldflags -X main.version=…`.

## 2. `internal/cli` — the exit-status guard

Twelve lines of substance. `cli.Guard` wraps every registered command body and
records failure in a package-level `atomic.Bool`; `cli.Exit` reads it after
`start.Up()` returns. Without this, `qm render` could print its error and exit
0, and a script or CI job would take a failed render for a good one.

Every `start.Command` in the repository has `Cmd: cli.Guard(cmd)`. That is the
convention to preserve when adding a command.

---

## 2a. `internal/config` — `qm.yml`

The project's vocabulary for rendering. Read this before `build`.

- `Load` (`config.go`) reads `qm.yml` from the project root with unknown keys
  as errors, then `complete` fills the defaults and checks cross references.
  Without a file the project gets one topic per top-level content folder
  (`ContentFolders`), the formats `web` (html) and `docx`, and one audience,
  `all`. A format's `type` is a `StringList` (`html`, `docx`, `pdf`, or a list
  of the last two), guessed from the format's name when left out.
- `Entries[T]` (`entries.go`) is an ordered YAML map: the order of `formats`,
  `audiences` and `topics` is the order of the matrix and of the web panel.
- `Config.Expand` (`matrix.go`) turns a `Request` (topics, formats, audiences)
  into `Selection`s. A document format yields one selection per topic and
  audience; a website yields one per audience, with all requested topics in
  `Selection.Topics`. A topic and a format each may narrow the axes from their
  side, and both have to agree. Unknown names are errors.
- `Selection` is one render. `Vars` merges the project's, topic's,
  audience's and format's variables (later wins) and adds the built-ins
  `topic`, `format` and `audience`; `Interpolate` replaces `{name}`
  placeholders and treats an undefined one as an error. `OutputDir`,
  `FileStem`, `Cover` and `Template` apply it to the format's and topic's
  paths.
- `Selection.Keep` is the conditional-content decision: format classes and
  audience classes are checked separately and both must pass. `Types` gives
  the names Quarto's `when-format` conditions compare against.

---

## 3. `internal/qmcore` — front matter and page scans

Per `spec.yaml` MAIN.2-4 the user-facing object name (`chapters`) is
deliberately *not* a package name. The package once held the Quarto profile
vocabulary — axes, `ParseSelection`, `LoadProfile`, `BuildMatrix`, in
`selection.go`, `profile.go`, `profileconfig.go` and `matrix.go`. Rendering no
longer uses any of it; `internal/config` replaced it. It stays only because
`qm migrate` reads the old profile files with it (`LoadSelection`,
`AxisValues`), and can go once migrating is no longer needed.

### 3.1 Front matter, on disk — `scan.go`, `profile_yaml.go`, `types.go`

`ReadFrontmatter` (`scan.go`) scans a `.qmd` for its leading YAML block and
unmarshals the three fields qm cares about (`order`, `insert-at`, `title` —
`types.go:18`). A file without front matter is not an error, it is a zero
value.

`UpdateFrontmatter` (`scan.go`) is the write side, and the reason the
`chapters` subcommands do not mangle files: it parses the block into a
`yaml.Node`, hands the node to a mutation callback, and writes it back with the
body preserved *verbatim*. The node-level helpers the callbacks use —
`SetMappingScalar`, `RemoveMappingKey`, `RenameMappingKey` — are in
`profile_yaml.go`.

`ScanFiles` (`scan.go:76`) walks a content folder and applies the audience
filter. `ApplyVariantFilter` (`scan.go:142`) implements the `_FW`/`_POL` rules
of `spec.yaml` BOOKS.4 in three passes: drop folders whose suffix does not
match the audience; drop plain folders superseded by a variant folder; and
group files by their base name so that `x_POL.qmd` supersedes `x.qmd` for the
`pol` audience. Every supersession prints a warning to stderr.

`ScanFolderChapters` (`build.go:26`) is the odd one out: a single-folder,
non-recursive scan that returns one `ChapterItem` per chapter, where a chapter
is either a `.qmd` in the folder or a subfolder represented by its `index.qmd`.
It exists purely for `chapters insert|move|remove`, which reason about one
folder's order list. Its sort is deliberately total: by order, then by path,
with unordered entries last.

---

## 4. `internal/bookmaker` — turning a folder into one document

The answer to fact 1, and the densest code in the repository. It is a
copy of the `quarto-bookmaker` command's internal package (MIT), which Go's
`internal` rule prevents importing directly — see `doc.go`.

Read it in this order.

### 4.1 The tree — `tree.go`

`LoadTree` (`tree.go:99`) reads a content folder into a `Node` tree. A `Node`
(`tree.go:30`) is either a directory (which may carry an `index.qmd` as its own
content) or a standalone page.

Three rules are worth noticing:

- **Heading level comes from folder depth.** `levelForDepth` (`tree.go:140`)
  maps depth onto a heading level: the book root's own index page and the first
  level of subfolders are both chapters (level 1), each further nesting level
  adds one, capped at 6.
- **Ordering follows Quarto's own rule** (`spec.yaml` ORDERS): a directory
  takes the `order:` of its `index.qmd` and sorts at its *parent's* level
  (`sortKey`, `tree.go:65`); ties fall back to a case-insensitive name compare
  (`sortNodes`, `tree.go:219`).
- **What Quarto ignores, the bookmaker ignores**: names starting with `.` or
  `_` (`skipEntry`, `tree.go:246`), and directories holding no `.qmd` anywhere
  below them, which are media folders and must not become empty chapters
  (`prune`, `tree.go:114`).

`audienceOf` (`tree.go:309`) reads the `_FW`/`_POL` suffix off a name;
`humanise` (`tree.go:319`) turns a slug into a readable heading for a page with
no title. `BookFolders` (`tree.go:305`) lists a project's content folders,
skipping media directories and sub-projects that carry their own
`_quarto.yml` — it backs the "the project has: …" hint in error messages.

### 4.2 Front matter — `frontmatter.go`

`splitFrontMatter` (`frontmatter.go:42`) tolerates a BOM and leading blank
lines, accepts `---` or `...` as the terminator, and treats an *unterminated*
block as body rather than guessing. `noOrder` (`frontmatter.go:27`) is the sort
key for pages without an `order:` — a large constant, so they sort last.

Only `title` and `order` are kept. Everything else in a page's front matter is
website metadata that means nothing once the pages are one document.

### 4.3 Anchors — `slug.go`

`anchorID` (`slug.go:19`) derives a heading identifier from the node's path,
prefixed `bm-` to stay clear of Quarto's reserved cross-reference prefixes
(`sec-`, `fig-`, …), which would turn the heading into a numbered reference.
Path slugs are not injective, so `assignAnchors` (`slug.go:50`) walks the tree
in render order and deduplicates with a counter — which also makes the ids
stable for a given source tree.

### 4.4 The flattening itself — `flatten.go`

`Flatten` (`flatten.go:76`) is the entry point: assign anchors, mark the root
as the book root, build the link target table, render.

Read the doc comment above it before the code. The generated documents
deliberately carry **no YAML front matter**: a `---` closing an inlined block
would read as a Setext underline and the document would be titled `title: …`.

`renderNode` (`flatten.go:101`) recurses depth-first and returns *two* strings
side by side: the book text and the slide text. (Document renders ask for slide
extraction only so that `::: slide` blocks stay out of the book; the deck itself
is not built, PPTX being unsupported for now.) Audience wrapping happens here
(`flatten.go:131`): content from an `_FW`/`_POL` folder is wrapped in a
matching `::: fw` / `::: pol` div so the project's content filter can select it
at render time (it is a conditional div like any other, kept when the selected
audience is named `fw` or lists the class) — once per audience change, not once per nesting level, which is
what `parentAudience` tracks.

`renderPage` (`flatten.go:149`) is the subtle part. It has to decide whether a
page already carries its own title heading, because pages come in both shapes:
some repeat the front-matter title as a heading inside every content div,
others give only sub-headings. Treating the shallowest heading as the page
title in the second case would promote a section to chapter rank. So a declared
title counts as present only when a heading at the shallowest level actually
spells it out (`echoesTitle`, `flatten.go:246`). If it does not, a heading is
generated and the body's headings stay below it.

Heading levels are **demoted by folder depth**, not normalised: a page keeps the
levels it was written with, plus the node's depth minus one (`delta` in
`renderPage`). Pages in the topic folder and a subfolder's `index.qmd` keep their
headings; each deeper folder adds one level. The website needs the same logic
page by page, so `Pages` and `PageMarkdown` render a single node's own page
with the same demotion and no link rewriting.

`preparePage` (`flatten.go:197`) is the per-page pipeline, and the order of its
steps is load-bearing:

1. `balanceFences` — make the page's markup self-contained.
2. `splitSlides` — take the deck out **before** anything else, because heading
   levels are what cuts a deck into slides and the book's anchors do not exist
   in it.
3. `rewriteLinks` — turn in-book links into anchors.
4. make relative `{{< include >}}` paths project-absolute
   (`absoluteIncludes`): a relative path is relative to the page, and once
   the page is part of a book or a site page the page's folder is no longer
   known.

`shiftHeadings` (`flatten.go:283`) applies the level delta and attaches the
anchor to the page's own title heading — per *block*, not per page: sibling
content divs (`::: explanation`, `::: tutorial`) typically repeat the heading
verbatim and only some survive the content filter, so each repetition claims
the id and the markdown package's `AssignIDs` numbers whichever duplicates
actually survive the filter.

### 4.5 The Markdown surgery — `markdown.go`

The low-level layer everything above stands on. Its header comment lists what
it does; the pieces to understand are:

- `codeTracker` (`markdown.go:93`) follows fenced code blocks across lines, so
  that **nothing inside code is ever treated as markup**. Every scanner in the
  package starts with `code.step(line)`.
- `balanceFences` (`markdown.go:158`) closes what a page left open and drops
  stray closing fences. A parser tolerates imbalance because it closes everything
  at end of file — but concatenating such a page would swallow the rest of the
  book into a code listing or a div.
  It also inserts the blank line an opening div fence needs
  (`continuesParagraph`, `markdown.go:216`): a `:::` written straight under a
  paragraph is lazy continuation and stays *text*, while the fence meant to
  close it is still read as a fence, so it closes an enclosing div instead and
  every division after it is off by one. Invisible per page, catastrophic once
  concatenated.
- `findHeadings` (`markdown.go:244`) collects ATX headings with their
  top-level div block number (see the anchor-per-block rule above).
- `renderHeading` (`markdown.go:335`), `splitHeadingRest` (`markdown.go:367`)
  and `mergeAttrs` (`markdown.go:431`) rebuild a heading at a new level with
  merged attributes, preserving an identifier the source page declared itself.
  The `headingAttrs` regexp (`markdown.go:43`) carries the trickiest comment in
  the file: the discriminator against a bracketed span at the end of a heading
  (`## [Wartenwand]{.fw}`) is not whitespace but the preceding `]`.

### 4.6 Slides — `slides.go`

`splitSlides` (`slides.go:50`) walks a page's lines and pulls the content of
every `::: slide` div out into a second document. Nothing renders that document
now; it is kept for the follow-up that brings PPTX back. The fences themselves are
dropped — what remains is the slide's own Markdown, whose heading levels are
the deck's structure and are therefore left exactly as written. Divs
*enclosing* a slide are reproduced around the extracted content (`enclose`,
`slides.go:145`), so a slide inside `::: pol` stays marked for that audience
once it stands alone. `isSlideFence` (`slides.go:26`) compares per token, so
`::: {.slide-notes}` is not a slide.

### 4.7 Links — `links.go`

`buildLinkTargets` (`links.go:21`) registers every URL spelling under which a
page can be referenced — a website page is reachable as `/a/b/`, `/a/b`,
`/a/b/index.qmd` and `/a/b/index.html` — and maps them to the page's anchor.
`lookup` (`links.go:45`) resolves a destination and returns early for anything
leaving the project (schemes, protocol-relative URLs, bare fragments). A link
that carries its own fragment keeps just the fragment: the target heading's own
identifier survives into the merged document.

`rewriteInlineLinks` (`links.go:130`) scans rather than pattern-matches,
because Markdown inline links stop being a regular language as soon as
`[![alt](/img.png)](/page/)` shows up. For every `](` it walks backwards with a
depth counter to find the matching `[`, which is what distinguishes an image
from a link and an inner image from its enclosing link (`isImageLink`,
`links.go:182`).

---

## 5. Parsing and writing

Everything between the flattened text and the file on disk.

### 5.1 `internal/shortcode`

`Expander` resolves the shortcodes the content uses, on the text, before it is
parsed: `{{< include path >}}` (project-absolute with a leading `/`, else
relative to the including file; nested up to eight deep, cycles are errors), `{{< pagebreak >}}` (an empty `::: {.qm-pagebreak}` div that
every writer turns into its own page break) and `{{< var name >}}`. Text in
fenced code is left alone, and any other shortcode is reported once and left
as written. `Variables` builds what `var` resolves against: `_variables.yml`
(nested keys addressed with dots), overlaid with the selection's variables.

### 5.2 `internal/markdown`

`New` (`markdown.go`) configures goldmark with GFM tables, strikethrough,
footnotes, definition lists, and qm's own pieces: a fenced-div block parser
(`div.go`), a bracketed-span inline parser (`span.go`), attribute blocks on
headings, links and images (`attrs.go`), and an AST transformer that turns
callout divs into callouts. `html.go` renders the same tree to HTML for the
website. `codeTracker` wraps the fenced-code parser so that nothing inside code
is ever read as a div.

`filter.go` is where a selection takes effect. `Prepare` runs `Filter` — drop
every div or span that `Keep` rejects or that a `.content-visible` /
`.content-hidden` condition hides, and always drop `slide` and `notes` — then
settles callout titles, then `AssignIDs` (after the filter, so that only
surviving headings are counted). `Demote` shifts heading levels.

### 5.3 `internal/build`

`Render` (`build.go`) produces one selection. For a website it calls
`renderSite`. For a document it checks the cover and the template, then
`Document` — `Flatten` (load the topic tree, `bookmaker.Flatten`, expand
shortcodes) followed by `markdown.Parse` and `Prepare` — and writes one file per
type of the format: `docx.Write` plus `docrender.CoverDOCX`, or
`docrender.RunPDF` with the parsed document as input and the fonts that
`brandFonts` (`brand.go`) found in `_brand.yml`. `Run` walks the selections,
carries on after a failure so one broken combination does not hide the others,
and after `--clean` empties each output directory once, refusing one that is not
below the output directory. `Flatten` is also what `qm flatten` calls.

`Logf` is the callback the whole flow reports through, which is what lets it
print to a terminal from `qm render` and to a polled HTML panel from `qm web`.

### 5.4 `internal/site`

`Build` writes the website of a set of topics: each topic's tree
(`bookmaker.LoadTree`) is loaded by `builder.load`, every page is rendered with
`bookmaker.PageMarkdown` (same demotion as a document), parsed, filtered with the
selection's `Keep`, and put into `assets/page.html`. `nav.go` builds the
navigation data: the navbar with one entry per topic, the topic's sidebar tree and
the previous/next links; `tocOf` takes the page's headings for the "On this page"
list. `links.go` rewrites links between pages and images to relative URLs and
copies the project files they point at. `assets/style.css` and `site.js` (scroll-
spy, off-canvas sidebar) are embedded; the layout is in em throughout, with its
breakpoints at 75em and 50em.

### 5.5 `internal/docx`

`Write` generates the document body as WordprocessingML from the parsed tree and
places it into a base package — the format's `template:` DOCX (styles, page setup,
headers, footers and numbering are kept, its body is discarded) or else a blank
document made with docxgo. docxgo has no bookmarks, internal hyperlinks,
footnotes or table shading, so `blocks.go` and `inline.go` write the body as raw
OOXML, `styles.go` maps elements to the template's style names, and `package.go`
patches the relationships, content types and settings.

### 5.6 `internal/docrender`

The PDF layout (gopdf) and the DOCX cover. The layout works on its own
`Block`/`Run` model; `FromMarkdown` (`markdown.go`) converts the parsed tree into
it, so the PDF is fed from the same document as the DOCX. `fonts.go` loads the Go
fonts and the `_brand.yml` fonts, `cover.go` puts the cover PNG in front of
either file.

---

## 6. A render, end to end

### 6.1 `qm render`

`render/render.go`. `cmd` collects the flags, appends the positional arguments to
`--topic` (topics being the axis one usually varies) and hands a `config.Request`
to `Run`. `BuildOptions` loads `qm.yml`, expands the request with
`Config.Expand` and returns the `build.Options`; `Run` calls `build.Run` with a
logger that prints to stdout. That is all `qm render` is. `qm web` builds the same
`build.Options` through `server.selections` and runs them in its background `job`.

### 6.2 `qm flatten`

`flatten/flatten.go`. `Run` loads the config, looks the topic up and calls
`build.Flatten` with the first format and audience, then writes
`_build/<topic>.md`. It is the way to look at what a document render parses —
heading demotion, rewritten links, resolved shortcodes — without writing a
document. The conditional content is still in it; removing it needs a
selection.

### 6.3 `qm migrate`

`migrate/migrate.go`. The old setup was one `_quarto.yml` plus one profile file
per axis value. `Run` reads them with `qmcore` (and `rawQuarto` for the keys qmcore
does not know), builds the equivalent `qm.yml` as a `yaml.Node` tree so that key
order and comments survive, turns `{{< var x >}}` into `{x}`, collects warnings
for everything it cannot carry over (slide formats, variables in titles, a
template generated into `_build/`, output directories outside the common
parent), and verifies the result with `config.Load` before writing it. It refuses
to overwrite an existing `qm.yml` without `--force`; `--dry-run` prints it.

---

## 7. The `chapters` subcommands

Three small commands that edit `order:` front matter in one folder. All three
share the same shape: resolve `--project`, resolve the folder argument, call
`qmcore.ScanFolderChapters`, then rewrite front matter through
`qmcore.UpdateFrontmatter`. None of them touches a chapter list — there is
none to touch, because a document is its folder flattened at render time.

- **`insert`** (`insert/insert.go`). The new chapter is a file the user has
  already created, carrying `insert-at: <n>` and no `order:`.
  `findInsertCandidate` (`insert.go:95`) insists on exactly one such file,
  `renumberForInsert` (`insert.go:127`) shifts everything at `order >= n` by
  one, and `convertInsertAtToOrder` (`insert.go:155`) renames the key.
- **`move`** (`move/move.go`). `Run` (`move.go:70`) locates the chapter at
  `oldOrder` and computes a new order for every affected sibling: moving up
  shifts `[new, old)` by +1, moving down shifts `(old, new]` by −1
  (`move.go:105`). The updates are collected into a map and applied in one
  pass.
- **`remove`** (`remove/remove.go`). `Run` (`remove.go:72`) takes `io.Reader`
  and `io.Writer` so the confirmation prompt is testable. It prints file, order
  and title, asks, and then `trashOrRemove` (`remove.go:139`) tries the
  external `trash` CLI before falling back to permanent deletion. A directory
  chapter deletes its whole folder, since its front matter lives in the
  `index.qmd` inside it.

---

## 8. `qm lint`

`lint/lint.go`. `Run` (`lint.go:72`) collects the files — the whole project
(`allDocFiles`, `lint.go:158`) or one topic's content folder (`topicFiles`,
`lint.go:116`) — and runs every check over each of them, printing findings as
`path:line: message` to stderr and returning an error when there is at least
one.

The argument is a *topic*, not a whole selection: linting looks at source
files, which are the same for every format and audience. `--lint-audience` is
an optional narrowing to one set of `_POL`/`_FW` variants. The topic's folder comes from `qm.yml` (`config.Load`), or is the topic name.

There is currently one check. `CheckFences` (`lint.go:210`) tracks opening
fences (`:::` plus a block descriptor) on a stack and closing fences (`:::`
alone) as pops, then reports every opener still on the stack at end of file.
A closing fence with nothing to close is ignored here by design.

Adding a second linter means writing a `Check…` function returning
`[]Finding` and calling it in the loop at `lint.go:89`.

---

## 9. `qm web` — the local UI

`qm web` is the [quarto-sorter](https://github.com/christophberger-ailab/quarto-sorter)
tool merged into qm. Its render panel drives the same `internal/build`
flow `qm render` does, so both produce identical output.

### 9.1 Entry point — `web/web.go`

`Register` (`web.go:36`) adds the `--addr` flag; `cmd` (`web.go:53`) lets a
positional path win over `--project`, so `qm web ../book` reads as it looks.
`Serve` (`web.go:72`) builds the server and blocks in
`http.ListenAndServe`.

### 9.2 The server — `web/server.go`

`server` (`web/server.go:28`) holds the currently open project behind a single
mutex — this is a single-user local tool — plus three things with their own
locks because they must not block the tree handlers: the on-disk fingerprint,
the background render `job`, and the `searchIndex`.

Everything is embedded: `//go:embed assets` (`server.go:23`) bakes the
templates and static files into the binary, so nothing is read from disk at run
time.

`newServer` (`server.go:58`) is the route table, and the fastest way to see
what the UI does:

| route | handler | purpose |
| --- | --- | --- |
| `GET /{$}` | `page` | the app shell |
| `GET /tree`, `GET /watch` | `treeHandler`, `watch` | the page tree; `/watch` is polled and answers `204` unless the project changed on disk |
| `POST /move`, `/create`, `/delete` | | tree edits, all routed through `apply` |
| `GET /content`, `POST /save` | | the editor pane |
| `GET /media/{path...}` | `media` | images for the preview |
| `GET /search` | `search` | the top-bar search |
| `POST /render`, `/render/select`, `GET /render/status` | | the render panel and its log |
| `GET /config`, `/config/preview-css`, … | | the custom preview stylesheets |

Two mechanisms are worth reading closely:

**The fingerprint.** `fingerprint` (`server.go:271`) hashes the paths, sizes
and mtimes of the project's `.qmd` files and its `qm.yml`.
`rememberFP` (`server.go:303`) records it after every response the server
produces itself, so `/watch` stays quiet for the app's own writes and fires
only on outside changes. It is also where the search index learns the project
moved on.

**`apply`** (`server.go:488`) is the shape of every tree mutation: load a fresh
tree, run the operation, re-render the tree fragment — with the error as a
banner above the (reverted) tree if the operation failed.

The rest is htmx plumbing: out-of-band swaps (`renderTreeOOB`,
`server.go:758`) refresh the tree pane from a response whose main target is
somewhere else.

### 9.3 The page tree — `internal/project`

A separate package from `bookmaker`, and deliberately so: it reads and *edits*
the website's page tree, where `bookmaker` only reads it to flatten it. It came
from quarto-sorter (see `internal/project/doc.go`).

`Load` (`tree.go:44`) walks the project for `.qmd` files, parses each one's
front matter, and assembles a `Page` tree. The interesting rules: `name/index.qmd`
represents its directory and sorts among its *parent's* pages; `name.qmd` next
to a `name/` directory does the same when there is no index; markers (🚒/🚔) are
derived from `_FW`/`_POL` suffixes and inherited by children (`markPages`,
`tree.go:179`). A page's `draft: true` rides along as `Page.Draft`, which is
what the tree template greys the entry by — the page stays in the tree, since
it is still there to be opened, moved, and searched.

`Move` (`move.go:19`) is the drag-and-drop operation, and it keeps three things
in sync at once: it moves files when the parent changes (`reparent`,
`move.go:69`), shifts the moved page's Markdown headings by the depth
difference (`shiftHeadingsBelow`, `move.go:119`), renumbers the destination
siblings sequentially, and closes the order gap left behind in the source
group. `CreatePage`, `CreatePageAfter`, `DeletePage` round out the editing API;
`trash` (`move.go:225`) again prefers the external `trash` command.

`frontmatter.go`, `heading.go`, `fences.go` are the byte-level helpers:
`SetOrder`, `ShiftHeadings`, `FirstHeading`, `BalancedFences` — the last one
feeding the tree's `BadFences` flag, which is the same check `qm lint` runs.
`FirstHeading` drops the heading's own Pandoc attribute block, so
`# Schulungen {.unnumbered .unlisted}` titles a page "Schulungen"; a `{...}`
right after a `]` is a bracketed span's and is left alone, the same rule
`bookmaker`'s `heading.Text()` applies.

### 9.4 Background work — `web/job.go`, `web/search.go`

`job` (`job.go:13`) is the single background render: `start` (`job.go:51`)
refuses to start a second one, the goroutine appends lines through `logf`, and
the panel polls `state()` for a consistent snapshot. `build.Run` receives
`j.logf` as its `Logf` — the same callback interface the CLI fills with
`fmt.Printf`.

`searchIndex` (`search.go:24`) is the same pattern applied to the word index:
`rebuild` (`search.go:76`) schedules a build and returns at once, a build
already running is not interrupted but picks up the newer state when it
finishes (so a burst of saves costs one extra pass, not one per save), and
`search` (`search.go:131`) answers from whatever the index currently holds,
telling the client to ask again while a build runs.

What the index holds is `wordIndex` (`search.go:49`), word → page →
positions, with the words as they are written plus a lowercased lookup, so a
search can ignore case or, with the case button pressed, match it. The positions are what make a quoted phrase answerable: a phrase
is the places its first word appears, minus the ones its next word does not
carry on from. `indexWords` (`search.go:220`) is the tokenizer both sides of
the wire use — Markdown and YAML syntax separates words, a single hyphen
inside one does not, and everything that is neither punctuation nor
whitespace is a word character, emoji included. `parseQuery`
(`search.go:278`) reads a query into loose words and quoted phrases;
`app.js` reads it a second time for the editor's highlighting, and
`TestParseQueryReadsQuotesAndWords` is where the two readings are kept
honest.

### 9.5 Settings — `web/settings.go`, `web/migrate.go`, `web/prefs.go`

Everything the UI remembers between runs is one value, `storedConfig`
(`settings.go`), in one file: `<user config dir>/qm/config.cue`. It holds the
recently opened projects, the per-project render selection and last open page
(`projectPrefs`, `prefs.go`), which preview stylesheet is active, and the
copyediting setup. It is written with mode 600, because the connections in it
hold API keys.

The file is CUE for one reason: **it carries its own schema.** `configSchema`
is a Go constant written into every file above the settings, and every read
unifies the settings with it and calls `Validate(cue.Concrete(true))`. So the
file tells the user what belongs in it, and a mistake in it is reported with
the file, line and column — a misspelled field included, since CUE definitions
are closed. Keeping the schema in a Go constant and rewriting it on every save
is what stops the file and the binary drifting apart.

Two rules protect the only copy of the user's connections and selections. A
file that does not check out **stops the app** (`newServer` returns the error,
`cli.Guard` makes the exit status non-zero) and is left exactly as it stands,
rather than being replaced by defaults. And `saveConfigFile` reads back what
it is about to write before it replaces the file: it is our own schema on both
sides, so it should never fail, which is precisely why it is worth finding out
before the file is overwritten rather than at the next start.

Lists and maps in the schema carry no `| *[]` default and scalars do. That is
not a style choice: an omitted list is already the empty list under
`cue.Concrete`, while the disjunction would turn one bad element into "N errors
in empty disjunction" and bury the real one.

`migrate.go` reads the JSON files qm used to keep — `render.json`,
`recent.json`, `copyedit.json`, and the `custom-css/.active` marker — into the
new file the first time it runs without one, then renames them `*.migrated`:
no longer read, not thrown away either. `legacyConnection` exists only because
the old file spelled the base URL `base_url` where the schema spells it
`baseURL`.

What is left in `prefs.go` is the custom preview stylesheets in
`<user config dir>/qm/custom-css/`, which stay files of their own because they
are CSS: `ensureDefaultCSS` materialises the baked-in default once and never
rewrites it, and `sanitizeCSSName` keeps user-supplied names to a safe shape.
*Which* of them is active is a setting, so it moved into the settings file.

### 9.5.2 Copyediting — `web/copyedit.go`, `web/llm.go`

The copyedit pane beside the editor runs an *editing task* — a prompt with a
title — over the page the editor holds. `copyedit.go` keeps both halves of the
setup: the tasks, and the API connections they run on. Both are the user's own
rather than the project's, which is why they live in the settings file
(§ 9.5) and not in the tree. `connectionView`
(`copyedit.go:227`) is what the pages and the pane are rendered from, and it
has no key field at all: the key travels to the API and nowhere else.

`copyeditRunHandler` reads the selected tasks and the connection under the
lock, then drops it before calling the model — a run takes as long as the
model takes, and the tree, the editor and the autosave must stay answerable
meanwhile. The text it edits comes from the request, not from disk: htmx sends
the editor's own textarea, so unsaved edits are edited too. The form may name
several tasks (the checkboxes in the pane), and they are collected in the
order the config lists them, so the groups read the way the task list does.

Several tasks go in **one** call rather than one call each, which is where the
cost of a run actually sits. The page is the bulk of what a request carries:
sending it once for five tasks instead of five times cuts a run's input by
roughly three quarters. What that saving must not buy is output nobody asked
for — output is priced several times higher than input per token — so only the
ticked tasks are ever sent; running all of them by default would cost *more*
than running the two the user wanted. `groupSuggestions` sorts the answer back
into the tasks it came from, and `taskOf` (`llm.go`) does the attributing: a
tag naming no task of the run leaves its suggestion unattributed rather than
dropped, and a run of one task needs no tag at all.

Which tasks are ticked is a setting too (`copyeditConfig.Selected`), not
something the browser holds: the pane is re-rendered with every page switch,
so ticks kept in the DOM would be lost on each one, and the same handful of
tasks tends to be run over page after page. `selectedTasks` filters the saved
ids against the tasks that exist, so a task deleted since — or an id written
into the settings by hand — is neither offered nor run. In the browser,
`applyTaskSelection` (`app.js`) puts the Select all box into the in-between
state when only some are ticked, which is a state only script can set.

Which way a run is carried out is a setting (`copyeditConfig.Mode`, the two
`mode*` constants), because the cost argument above is only half the story:
one call for five tasks is cheaper, but a model given five instructions
answers with one list that tends to be shorter than five lists would be, and
whether that costs findings is a property of the model and the tasks rather
than something to assume. `runCopyeditPerTask` is the other way — one call per
task, **in sequence**, not in parallel: the page is the cached part of the
request, a cache is written by the call that misses it, so firing the tasks at
once would have them all miss where in sequence the first writes it and the
rest read it (a burst is also what a rate limit answers with 429s). A task
whose call fails is reported beside what the others found; only a run in which
nothing succeeded is an error. Each list of suggestions carries the mode that
produced it, so two runs of the same tasks can be told apart.

`llm.go` is the call itself, in net/http and encoding/json alone. Two request
shapes cover what a connection can point at (`requestFor`): Anthropic's
`/messages` and the OpenAI-style `/chat/completions` every other provider
speaks. Both are ordered instruction, page, tasks, and the order is the point:
a prompt cache matches on a request's prefix, so everything up to the tasks is
identical from one run to the next on the same page and can be served from the
run before it. The other way round — the task first, as the first version had
it — the page would sit behind the one part that changes every run and could
never be cached at all. On Anthropic the prefix must also be marked, which is
the `cache_control` breakpoint on the page block. `answerBudget` grows
`max_tokens` with the number of tasks, since an answer cut off mid-JSON loses
the whole run rather than one task of it. `suggestionFormat` is the part of the instruction that is ours rather
than the user's — it asks for JSON, and for each suggestion to quote the
passage it applies to, verbatim and short, because a passage that cannot be
found again cannot be highlighted. `answerText` is deliberately forgiving about what comes back, because
"the model answered with no text" was true of four quite different
situations and helped with none of them: an error the gateway put in a 200
body (reported as that error), a reasoning model that spent the whole token
ceiling thinking (said as that, which is also why `maxTokens` leaves room to
think rather than being sized for the suggestion list alone), one that
answered in its reasoning and never wrote a final message (read from the
reasoning), and content sent as parts rather than a string (`messageText`
takes either). Anything still textless reports the stop reason and the body,
since no guess beats showing what the provider sent.

`decodeSuggestions` reads the answer
leniently (a code fence, a line of prose above the JSON, "suggestion" spelled
"replacement"), and `locateAll` finds each quoted passage in the page: as it
stands, else with any run of whitespace where the quote has one, since a model
rarely reproduces a line break. Positions come out in UTF-16 code units
(`utf16Len`), which is what a browser counts string positions in — the editor
marks by them, and an emoji ahead of a passage would otherwise shift the mark
off it.

### 9.5.1 Tree titles — `web/spans.go`

The preview marks a Quarto span by its class, with the symbol the stylesheet
puts in front of it (`.quarto.pol:before { content: "🚔" }`). The tree has no
stylesheet to lean on — its titles are text — so `spanSymbols` reads those very
rules out of the *active* stylesheet and `flattenSpans` rewrites
`[Polizei-]{.pol}[Feuerwehr-]{.fw}Spickzettel` into
`🚔Polizei-🚒Feuerwehr-Spickzettel`. Reading the CSS instead of keeping a table
of classes is the point: a user who edits `custom.css` moves the tree with the
preview, and there is nothing to keep in step by hand. A span whose classes no
rule names is left as written, so nothing disappears unannounced. `load`
(`server.go:231`) applies it to the freshly loaded tree it is about to render;
the tree on disk never sees it.

### 9.6 The front end — `web/assets`

`assets/templates/app.html.tmpl` holds every template the server renders by
name (`page`, `main`, `treewrap`, `content`, `render`, `render-log`,
`config-page`, `preview-css-page`, …). `assets/static/` holds `app.css`,
`app.js`, the preview and editor scripts, and the vendored libraries: htmx for
the interactions, Sortable for the drag and drop, marked for the live Markdown
preview, and CodeMirror 5 for the editor (with a Vim keymap behind a toggle).
`assets/static/codemirror/README.md` documents exactly which files were taken
from which tarball paths and why CodeMirror 5 rather than 6.

The preview and the copyedit pane beside the editor are laid out by
`applyLayout` (`app.js`): which of them are shown, in which order, and how
wide, all kept in localStorage, since the editor pane is re-rendered on every
page switch. The panes never move in the DOM; CSS `order` places them and the
divider on each one's left. The copyedit pane's suggestions carry the position of the passage each is about;
`readSuggestions` (`app.js`) hands those to `setCopyeditMarks`
(`editor.js`), which paints them with CodeMirror's `markText` — a mark is not
an edit, and CodeMirror moves it along as the text around it is typed. A
suggestion the server could not locate is passed as a gap, so the nth entry
and the nth mark stay the same suggestion.

`Done` is the other way a suggestion leaves the list: the user carried it out
by hand in the editor, so `clearCopyeditMark` takes that one mark off the text
and the entry stays as a record of what was seen to. It is offered on every
suggestion — the ones with no replacement and the ones whose passage was never
found included, those being exactly the ones only a human can carry out.

`Apply` writes a suggestion into the page, and it writes it *at the mark*
(`applyCopyeditMark`, `editor.js`), not at the offsets the suggestion arrived
with: the user may have typed since, and a mark moves with the text while an
offset does not. The replacement goes in through `cm.replaceRange`, which
makes it an ordinary edit — the textarea follows, the autosave posts it, the
preview catches up, and Ctrl-Z takes it back — and the mark is dropped
afterwards, the passage it stood for being gone. A passage deleted since the
run has no mark left to find, and the entry says "Gone" rather than offering a
button that does nothing. Which suggestions offer the button at all is decided
on the Go side: `suggestion.Applicable` (`llm.go`) wants a located passage and
a replacement to put there, so comment-only advice is read and carried out by
hand.

`preview.js` approximates a Quarto render rather than performing one, and two
of its rules come from elsewhere in the codebase: `convertDivs` mirrors
`bookmaker`'s fence handling, and `targetGroupOf` mirrors its `_FW`/`_POL`
suffix rule (`internal/bookmaker/tree.go:14`), wrapping the whole page in the
group's own `quarto` div so that the stylesheet marks a page written for one
audience the way it marks a block written for one. The deepest name decides,
as it does for the audience filter.

---

## 10. Tests and test data

Every package has a `_test.go` file beside its source; `spec.yaml`
IMPLEMENTATION.4 asks for red/green TDD. Two things are worth knowing before
adding to them:

- `testdata/project/` is a miniature project tree exercising the awkward cases:
  nested folders, an `_entwurf/` draft folder and a `.hidden/` one that must be
  skipped, a `dritter_FW/` audience folder, media directories, and a page whose
  slide div must end up in the deck rather than the book.
- `testdata/book.golden.qmd` and `testdata/slides.golden.qmd` are golden files
  for the flattener. A deliberate change to the flattening rules means
  regenerating them; an accidental one means the diff tells you exactly what
  moved.
- `internal/site` and `internal/build` tests render small projects into a
  temporary directory and check the generated HTML and the package parts of the
  DOCX, so no external tool is needed to run them.

Run everything with `go test ./...`; the release workflow additionally runs
`go vet ./...`.

---

## 11. Reading order, condensed

| # | file(s) | what you get |
| --- | --- | --- |
| 1 | `main.go`, `internal/cli/cli.go` | command wiring, exit status |
| 2 | `internal/config/config.go`, `matrix.go` | `qm.yml`, the three axes and the render matrix |
| 3 | `internal/bookmaker/tree.go`, `flatten.go` | folder tree → one document, heading demotion |
| 4 | `internal/bookmaker/markdown.go`, `slides.go`, `links.go` | the Markdown-level surgery |
| 5 | `internal/shortcode/`, `internal/markdown/` | shortcodes, the parser, the conditional filter |
| 6 | `internal/build/build.go` | one render: flatten, parse, filter, write |
| 7 | `internal/site/`, `internal/docx/`, `internal/docrender/` | the three writers |
| 8 | `render/`, `flatten/`, `migrate/` | the commands on the render path |
| 9 | `insert/`, `move/`, `remove/`, `lint/` | the standalone commands |
| 10 | `web/`, `internal/project/` | the local UI |

If you only read four files, read `internal/config/config.go`,
`internal/bookmaker/flatten.go`, `internal/markdown/filter.go`, and
`internal/build/build.go`. Everything else is either wiring around them or a
consequence of what their comments explain.
