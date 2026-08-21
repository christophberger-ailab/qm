# A walkthrough of the qm source

This document reads the code once, from front to back, in the order the
pieces depend on each other. It follows one `quarto render` from the command
line down to the renamed output file, then doubles back for the commands that
do not take part in a render (`chapters`, `lint`) and for the web UI.

Every section names the files it covers, so it can also be used as a map.

---

## 0. The five Quarto facts the code is built around

Almost every design decision in this repository is a reaction to something
Quarto does or refuses to do. Knowing these five up front makes the rest read
as consequences rather than choices:

1. **A Quarto book is flat.** One file per chapter, folder depth carries no
   meaning, and the first chapter must be the project root's `index.qmd`. A
   multi-level content folder cannot be expressed as a chapter list at all.
2. **Quarto resolves `{{< var >}}` in exactly two configuration keys**,
   `book: output-file` and `book: title`. Not in `project: output-dir`, not in
   format-level keys such as `format: pptx: reference-doc`.
3. **Profiles merge, they do not replace.** Two profiles of the same group are
   accepted silently and their array keys — `book: chapters` above all — are
   concatenated, not overridden.
4. **A missing profile file is ignored.** A typo in `--profile` produces no
   error; the variables that profile should have defined resolve to the
   literal `?var:audience`, which then lands in the output file name.
5. **Quarto aborts a render when the pre-render hook exits non-zero.** That is
   the only lever qm has to stop a bad render before anything is written.

Fact 1 produces `internal/bookmaker`. Fact 2 produces the `qm:` profile
section, `qm prepare`'s copy step and `qm finalize` entirely. Facts 3 and 4
produce the validation in `internal/qmcore/selection.go`. Fact 5 produces
`internal/cli`.

The formal specification of all of this lives in `spec.yaml` and the
`spec-*.yaml` files, one per subcommand, in the [acai feature-spec
format](./acai-feature-spec.yaml). The code comments reference them by
requirement id (`SUBCOMMANDS.3`, `BOOKS.2-1`, …).

---

## 1. `main.go` — the wiring

`main.go` is short and does four things.

**It declares the flags that are not owned by a single command.**
`--project`/`-p` (`main.go:38`) is qm-wide; `--profile` (`main.go:44`) is
shared by exactly two commands, the Quarto hooks `prepare` and `finalize`, so
it is declared once here rather than by either of them. Both are passed to the
subcommands as `*string`, which is the shape
[`github.com/christophberger/start`](https://github.com/christophberger/start)
uses — the flag is parsed by the time a command body runs.

**It models the `<object>` parameter.** The command pattern is
`qm <object> <subcommand> [flags]`, but `start` has no notion of an object. So
`chapters` is registered as a parent command with no `Cmd` of its own
(`main.go:59`), and `insert`, `move`, `remove` register themselves underneath
it. Leaving `Cmd` nil is deliberate: `start` then auto-generates the "missing
subcommand" usage instead of panicking.

**It registers everything else as a leaf command**: `lint`, `render`,
`flatten`, `prepare`, `finalize`, `web` (`main.go:76`–`main.go:93`). Each of
those packages exposes a single `Register(...)` function and keeps its flag
pointers in package-level variables — the same pattern in all eight command
packages, so once you have read one you have read them all.

**It sets the exit status.** `start.Up()` prints a command's error and returns
normally, leaving the status at 0. `cli.Exit()` (`main.go:102`) is what turns a
failure into a non-zero exit — see the next section.

`version` (`main.go:32`) is `"dev"` unless the release workflow stamps it via
`-ldflags -X main.version=…`.

## 2. `internal/cli` — the exit-status guard

Twelve lines of substance, and the reason for Quarto fact 5.

`cli.Guard` (`internal/cli/cli.go:28`) wraps every registered command body and
records failure in a package-level `atomic.Bool`; `cli.Exit`
(`internal/cli/cli.go:40`) reads it after `start.Up()` returns. Without this,
`qm prepare` could fail validation, print its error, exit 0 — and Quarto would
carry on rendering whatever the *previous* run left in `_build/`, producing the
last topic's content under the new selection's file name.

Every `start.Command` in the repository has `Cmd: cli.Guard(cmd)`. That is the
convention to preserve when adding a command.

---

## 3. `internal/qmcore` — the project's vocabulary

This is the package to read first after `main.go`, because everything else
speaks its language. Note the name: per `spec.yaml` MAIN.2-4 the user-facing
object name (`chapters`) is deliberately *not* a package name.

### 3.1 Axes and selections — `selection.go`

A render is addressed along three orthogonal axes, each of them an ordinary
Quarto profile:

```
topic-<t>     what is rendered    — which content folder
format-<f>    how it is rendered  — project type, output dir, output formats
audience-<a>  who it is for       — which ::: pol / ::: fw content survives

quarto render --profile topic-calltaker,format-handout,audience-pol
```

`Axis` (`selection.go:27`) is a string type with three constants; `Axes`
(`selection.go:39`) fixes their order, and the order matters — Quarto merges
profiles left to right and the first one wins for scalar keys, so `format` must
precede `audience` for a book's output directory to come from the format.

`Selection` (`selection.go:54`) is one value per axis. `ProfileNames`,
`String` (`selection.go:95`) turn it back into the `--profile` argument.

`ParseSelection` (`selection.go:109`) is the project's validation gate, and the
direct answer to Quarto facts 3 and 4: it rejects a name with no known axis
prefix, two profiles on one axis, and a missing axis. `SplitProfileName`
(`selection.go:150`) does the taking-apart, tolerating the `_quarto-` file
prefix. Because the axis comes from the *prefix* rather than a lookup table,
adding an audience means adding one `_quarto-audience-<x>.yml` and nothing
else.

`NoTopic` (`selection.go:50`) is the value `none`: a render of the whole
website, which selects no content folder.

`AxisValues` (`selection.go:171`) lists what an axis actually offers, by
looking at the profile files on disk. It is what the error messages and the web
UI's render panel are built from.

### 3.2 Finding profile files — `profile.go`

Small helpers: `ResolveProfilePath` (`profile.go:24`) accepts a bare name, a
`_quarto-`-prefixed name, or a full file name, preferring `.yaml` over `.yml`;
`NormalizeProfileArg` (`profile.go:38`) adds the prefix; `DiscoverProfiles`
(`profile.go:48`) lists every `_quarto-*.y[a]ml` in the project root.

### 3.3 Reading a profile — `profileconfig.go`

`Profile` (`profileconfig.go:83`) is a profile file as far as qm reads it:
`_quarto-vars`, `project: output-dir`, the `book:` keys — and `qm:`, the root
key Quarto ignores and qm owns.

`QM` (`profileconfig.go:39`) is worth reading field by field, because each
field is a workaround for Quarto fact 2:

| field | axis | why it exists |
| --- | --- | --- |
| `folder` | topic | the content folder to flatten; defaults to the topic name |
| `formats`, `audiences`, `topics` | topic / format | which combinations this profile takes part in — the render matrix |
| `copy` | format | target→source files copied into place before the render, because `format: pptx: reference-doc:` does not interpolate |
| `output-file` | format | renames a non-book artefact (a deck), which has no interpolating key |
| `output-dir-suffix` | format | appended after the render, because `project: output-dir` does not interpolate |

`LoadProfile` (`profileconfig.go:127`) reads one file and makes two failures
loud: a missing file (Quarto fact 4) and a profile that sets `book: chapters:`
(Quarto fact 3 — the list would be *added* to the one in `_quarto.yml`, and
the book would render twice).

`Profiles` (`profileconfig.go:161`) is the three profiles of one selection.
`Vars` (`profileconfig.go:194`) merges their `_quarto-vars` the way Quarto
merges profile configuration — first definition wins. `Folder`
(`profileconfig.go:211`) applies the `qm: folder:` default rule. `Title`
(`profileconfig.go:223`) resolves the book title, which is also the slide
deck's title, because the deck is assembled from divs that carry none.

`ResolveVars` (`profileconfig.go:239`) is qm's own `{{< var >}}` substitution.
Note that an undefined variable is an *error* here, not Quarto's `?var:name`
placeholder — the placeholder's only visible effect would be a wrongly named
file discovered hours later.

`ProjectPath` (`profileconfig.go:266`) joins a profile-supplied relative path
onto the project root and refuses to leave it. Every path that comes out of a
YAML file goes through it.

### 3.4 The render matrix — `matrix.go`

`BuildMatrix` (`matrix.go:44`) expands a request into the list of selections to
render. The full cross product is not it: a book has no website format, some
topics have no agency variants. Rather than encoding those exceptions in a
driver script, the *profiles declare their own participation* under
`qm: formats:` and `qm: audiences:`, and a profile that declares nothing takes
part in everything.

The rules, in code order: unknown names on any axis are errors, not empty
results (`checkKnown`, `matrix.go:164`); a topic's declarations narrow what is
available (`axisFor`, `matrix.go:131`); a format may exclude a topic or an
audience from its side, and both sides must agree (`matrix.go:91`); the
result order follows the project's own order on each axis rather than the order
a profile happened to spell its list in (`inOrder`, `matrix.go:153`).

### 3.5 Front matter, on disk — `scan.go`, `profile_yaml.go`, `types.go`

`ReadFrontmatter` (`scan.go:27`) scans a `.qmd` for its leading YAML block and
unmarshals the three fields qm cares about (`order`, `insert-at`, `title` —
`types.go:18`). A file without front matter is not an error, it is a zero
value.

`UpdateFrontmatter` (`scan.go:271`) is the write side, and the reason the
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

The answer to Quarto fact 1, and the densest code in the repository. It is a
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
deliberately carry **no YAML front matter**, because Quarto reads an included
chapter's opening heading with a line scanner that does not know it is looking
at an include — the `---` closing an inlined block reads as a Setext underline
and the book ends up titled `title: …`.

`renderNode` (`flatten.go:101`) recurses depth-first and returns *two* strings
side by side: the book text and the slide text. Audience wrapping happens here
(`flatten.go:131`): content from an `_FW`/`_POL` folder is wrapped in a
matching `::: fw` / `::: pol` div so the project's content filter can select it
at render time — once per audience change, not once per nesting level, which is
what `parentAudience` tracks.

`renderPage` (`flatten.go:149`) is the subtle part. It has to decide whether a
page already carries its own title heading, because pages come in both shapes:
some repeat the front-matter title as a heading inside every content div,
others give only sub-headings. Treating the shallowest heading as the page
title in the second case would promote a section to chapter rank. So a declared
title counts as present only when a heading at the shallowest level actually
spells it out (`echoesTitle`, `flatten.go:246`). If it does not, a heading is
generated and the body's headings are pushed one level down.

`preparePage` (`flatten.go:197`) is the per-page pipeline, and the order of its
steps is load-bearing:

1. `balanceFences` — make the page's markup self-contained.
2. `splitSlides` — take the deck out **before** anything else, because heading
   levels are what cuts a deck into slides and the book's anchors do not exist
   in it.
3. `rewriteLinks` — turn in-book links into anchors.
4. warn about `{{< include >}}` shortcodes, which Quarto resolves at render
   time and whose paths must therefore be project-absolute.

`shiftHeadings` (`flatten.go:283`) applies the level delta and attaches the
anchor to the page's own title heading — per *block*, not per page: sibling
content divs (`::: explanation`, `::: tutorial`) typically repeat the heading
verbatim and only some survive the content filter, so each repetition claims
the id and Pandoc renames whichever duplicates actually survive.

### 4.5 The Markdown surgery — `markdown.go`

The low-level layer everything above stands on. Its header comment lists what
it does; the pieces to understand are:

- `codeTracker` (`markdown.go:93`) follows fenced code blocks across lines, so
  that **nothing inside code is ever treated as markup**. Every scanner in the
  package starts with `code.step(line)`.
- `balanceFences` (`markdown.go:158`) closes what a page left open and drops
  stray closing fences. Pandoc tolerates imbalance because it closes everything
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
every `::: slide` div out into a second document. The fences themselves are
dropped — what remains is the slide's own Markdown, whose heading levels are
the deck's structure and are therefore left exactly as written. Divs
*enclosing* a slide are reproduced around the extracted content (`enclose`,
`slides.go:145`), so a slide inside `::: pol` stays marked for that audience
once it stands alone. `isSlideFence` (`slides.go:26`) compares per token, so
`::: {.slide-notes}` is not a slide.

### 4.7 Links — `links.go`

`buildLinkTargets` (`links.go:21`) registers every URL spelling under which a
page can be referenced — a Quarto website page is reachable as `/a/b/`, `/a/b`,
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

## 5. `internal/bookrender` — build documents and the Quarto driver

The bridge between the flattener and the `quarto` binary. Its doc comment
explains the one non-obvious constant: the generated documents live in
`_build/` — an underscore-prefixed **directory**, not under
underscore-prefixed *names*. The directory keeps Quarto's website scan out; a
name would also exclude the files from the project's *inputs*, and Quarto
honours `output-dir` and `output-file` only for project inputs, so a render
aimed at such a file falls through to the standalone document renderer and
writes its output next to the source.

The names are fixed (`book.qmd`, `slides.qmd`, `render.go:45`), which is what
lets `index.qmd` hold one topic-independent `{{< include >}}` and the topic be
chosen by the profile alone.

`WriteBuild` (`render.go:98`) is what `qm prepare` and `qm flatten` both call:
flatten the folder, log the warnings and the counts, write both files. An empty
folder (a website render, topic `none`) writes documents that hold nothing but
`generatedNote` (`render.go:55`) — a comment, because a heading would be picked
up as the chapter title, which Quarto reads before the content filters run.
`slideDoc` (`render.go:124`) is where the deck gets its title.

`Run` (`render.go:171`) walks a matrix, one `quarto` invocation per selection,
carrying on after a failure so one broken combination does not hide the others.

`quarto` (`render.go:216`) is nine lines with a long comment, and the comment
is the interesting part: the invocation is
`quarto render --profile <t>,<f>,<a> --no-clean`, run in the project root, with
**no input file, no `--to`, no `--output`, no `--output-dir`** — every one of
those is left to the configuration, which is the entire point of the
arrangement. `--no-clean` is not optional: several topics and audiences share
one output directory, so a cleaning run would delete what the run before it
produced. Since Quarto has no configuration key for that, cleaning becomes
`Options.Clean` and `cleanOutputDirs` (`render.go:240`), which empties each
format's directory once, before the first render.

`lineWriter` (`render.go:268`) feeds Quarto's stdout/stderr into the `Logf`
callback line by line — which is what lets the same flow print to a terminal
from `qm render` and to a polled HTML panel from `qm web`.

---

## 6. A render, end to end

Now the commands, in the order a real render touches them.

### 6.1 `qm render` — the matrix driver

`render/render.go`. `cmd` (`render.go:69`) collects the flags, appends the
positional arguments to `--topic` (topics being the axis one usually varies),
and hands a `qmcore.Matrix` to `Run` (`render.go:89`). `BuildOptions`
(`render.go:106`) expands it via `qmcore.BuildMatrix` and hands the result to
`bookrender.Run`.

That is all `qm render` is. The artefact is built and named by the project's
own Quarto hooks, so a plain
`quarto render --profile topic-x,format-y,audience-z` produces exactly the
same result without qm ever being invoked.

### 6.2 `qm prepare` — the pre-render hook

`prepare/prepare.go`, wired into the project as `project: pre-render: qm
prepare`.

`ActiveProfiles` (`prepare.go:95`) reads `--profile` or `$QUARTO_PROFILE` —
and the environment variable is the *effective* selection, with Quarto's own
group defaults already appended, which is what makes validating it worth
doing.

`Run` (`prepare.go:109`) then, in order:

1. `qmcore.ParseSelection` — one profile per axis (Quarto fact 3).
2. `qmcore.LoadSelection` — every named profile exists (Quarto fact 4).
3. `checkVariables` (`prepare.go:148`) — every `{{< var >}}` the selection
   relies on resolves. `interpolated` (`prepare.go:162`) enumerates the values
   to check: `book: title`, `book: output-file`, and each `qm:` key.
4. `checkFolder` (`prepare.go:184`) — the topic's content folder is really
   there; a typo would otherwise produce an empty book.
5. `bookrender.WriteBuild` — flatten the topic into `_build/`.
6. `copyFiles` (`prepare.go:204`) — carry out the `qm: copy:` map, resolving
   `{{< var >}}` in the source, so that a template depending on both format and
   audience can be pointed at by a fixed, non-interpolating format key.

Steps 1–4 are the half that matters most: each returns an error, `cli.Guard`
records it, the process exits non-zero, and Quarto stops (Quarto fact 5).

### 6.3 `qm finalize` — the post-render hook

`finalize/finalize.go`, wired as `project: post-render: qm finalize`. It reads
`QUARTO_PROJECT_OUTPUT_DIR` and `QUARTO_PROJECT_OUTPUT_FILES` (or the
equivalent flags — `envOr`, `finalize.go:100`) and applies the two rules a book
does not need but everything else does:

- `qm: output-file:` → `renameOutputs` (`finalize.go:196`) moves each produced
  file to `<output-dir>/<stem><ext>`. The move is to the *top* of the output
  directory on purpose: a `type: default` project mirrors the source layout, so
  a deck built from `_build/slides.qmd` arrives at
  `<output-dir>/_build/slides.pptx`. Emptied source directories are pruned
  (`pruneEmpty`, `finalize.go:245`).
- `qm: output-dir-suffix:` → `renameDir` (`finalize.go:226`) renames the output
  directory, which is how an audience-dependent `_output/site-pol` is reached
  given that `project: output-dir` does not interpolate (Quarto fact 2).

A format that declares neither returns immediately (`finalize.go:147`) — the
usual case for a book, whose `book: output-file` Quarto honours on its own.

### 6.4 `qm flatten` — the same, without Quarto

`flatten/flatten.go`. `Run` (`flatten.go:61`) loads only the *topic* profile
and calls `bookrender.WriteBuild`. It is the way to look at what a render will
feed to Quarto — heading demotion, rewritten links, extracted slides — without
waiting for pandoc. Because the other two axes are not read, the deck title
keeps whatever variables they would have supplied; resolving those is a
render's job.

---

## 7. The `chapters` subcommands

Three small commands that edit `order:` front matter in one folder. All three
share the same shape: resolve `--project`, resolve the folder argument, call
`qmcore.ScanFolderChapters`, then rewrite front matter through
`qmcore.UpdateFrontmatter`. None of them touches a book chapter list — there is
none to touch, because a book is its folder flattened at render time.

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
an optional narrowing to one set of `_POL`/`_FW` variants.

There is currently one check. `CheckFences` (`lint.go:210`) tracks opening
fences (`:::` plus a block descriptor) on a stack and closing fences (`:::`
alone) as pops, then reports every opener still on the stack at end of file.
A closing fence with nothing to close is ignored here by design.

Adding a second linter means writing a `Check…` function returning
`[]Finding` and calling it in the loop at `lint.go:89`.

---

## 9. `qm web` — the local UI

`qm web` is the [quarto-sorter](https://github.com/christophberger-ailab/quarto-sorter)
tool merged into qm. Its render panel drives the same `internal/bookrender`
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
and mtimes of the project's `.qmd` files and `_quarto*.yml` configs.
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

`Load` (`tree.go:43`) walks the project for `.qmd` files, parses each one's
front matter, and assembles a `Page` tree. The interesting rules: `name/index.qmd`
represents its directory and sorts among its *parent's* pages; `name.qmd` next
to a `name/` directory does the same when there is no index; markers (🚒/🚔) are
derived from `_FW`/`_POL` suffixes and inherited by children (`markPages`,
`tree.go:172`).

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

### 9.4 Background work — `web/job.go`, `web/search.go`

`job` (`job.go:13`) is the single background render: `start` (`job.go:51`)
refuses to start a second one, the goroutine appends lines through `logf`, and
the panel polls `state()` for a consistent snapshot. `bookrender.Run` receives
`j.logf` as its `Logf` — the same callback interface the CLI fills with
`fmt.Printf`.

`searchIndex` (`search.go:23`) is the same pattern applied to the word index:
`rebuild` (`search.go:55`) schedules a build and returns at once, a build
already running is not interrupted but picks up the newer state when it
finishes (so a burst of saves costs one extra pass, not one per save), and
`search` (`search.go:110`) answers from whatever the index currently holds,
telling the client to ask again while a build runs.

### 9.5 Preferences — `web/prefs.go`

`projectPrefs` (`prefs.go:20`) is what the UI remembers per project: the render
selection (topics, formats, per-topic audiences) and the page last open in the
editor. It is stored as JSON in `<user config dir>/qm/render.json` — still
called that because it began as the render selection alone, and renaming it
would drop every selection users have already made.

The rest of the file manages the custom preview stylesheets in
`<user config dir>/qm/custom-css/`: `ensureDefaultCSS` (`prefs.go:233`)
materialises the baked-in default once and never rewrites it,
`sanitizeCSSName` (`prefs.go:211`) keeps user-supplied names to a safe shape,
and `activeCSS`/`setActiveCSS` remember which stylesheet the live preview
uses.

### 9.6 The front end — `web/assets`

`assets/templates/app.html.tmpl` holds every template the server renders by
name (`page`, `main`, `treewrap`, `content`, `render`, `render-log`,
`config-page`, `preview-css-page`, …). `assets/static/` holds `app.css`,
`app.js`, the preview and editor scripts, and the vendored libraries: htmx for
the interactions, Sortable for the drag and drop, marked for the live Markdown
preview, and CodeMirror 5 for the editor (with a Vim keymap behind a toggle).
`assets/static/codemirror/README.md` documents exactly which files were taken
from which tarball paths and why CodeMirror 5 rather than 6.

---

## 10. Tests and test data

Every package has a `_test.go` file beside its source; `spec.yaml`
IMPLEMENTATION.4 asks for red/green TDD. Two things are worth knowing before
adding to them:

- `testdata/project/` is a miniature Quarto tree exercising the awkward cases:
  nested folders, an `_entwurf/` draft folder and a `.hidden/` one that must be
  skipped, a `dritter_FW/` audience folder, media directories, and a page whose
  slide div must end up in the deck rather than the book.
- `testdata/book.golden.qmd` and `testdata/slides.golden.qmd` are golden files
  for the flattener. A deliberate change to the flattening rules means
  regenerating them; an accidental one means the diff tells you exactly what
  moved.
- `bookrender.QuartoCommand` (`internal/bookrender/render.go:197`) is a
  variable so tests can substitute a stub for the real `quarto` binary.

Run everything with `go test ./...`; the release workflow additionally runs
`go vet ./...`.

---

## 11. Reading order, condensed

| # | file(s) | what you get |
| --- | --- | --- |
| 1 | `main.go`, `internal/cli/cli.go` | command wiring, exit status |
| 2 | `internal/qmcore/selection.go`, `matrix.go` | the three axes and the render matrix |
| 3 | `internal/qmcore/profileconfig.go` | what a profile file means to qm |
| 4 | `internal/bookmaker/tree.go`, `flatten.go` | folder tree → one document |
| 5 | `internal/bookmaker/markdown.go`, `slides.go`, `links.go` | the Markdown-level surgery |
| 6 | `internal/bookrender/render.go` | build documents, the `quarto` invocation |
| 7 | `prepare/`, `finalize/`, `render/`, `flatten/` | the render path |
| 8 | `insert/`, `move/`, `remove/`, `lint/` | the standalone commands |
| 9 | `web/`, `internal/project/` | the local UI |

If you only read four files, read `internal/qmcore/selection.go`,
`internal/qmcore/profileconfig.go`, `internal/bookmaker/flatten.go`, and
`internal/bookrender/render.go`. Everything else is either wiring around them
or a consequence of what their comments explain.
