# The qm command line in brief

`qm web` is one of several commands. They all work on the same Quarto project.

```
qm <object> <subcommand> [flags] [args]
```

## Global flag

| flag | environment variable | default | meaning |
| --- | --- | --- | --- |
| `--project`, `-p` | `QM_PROJECT` | `.` | path to the Quarto document tree |

## Commands

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

Every command takes `--help`, which lists its flags and arguments.

## In this help

- [Opening projects](projects.md) covers `qm web`.
- [Rendering](render.md) is the web version of `qm render`.

Back to [Contents](index.md).
