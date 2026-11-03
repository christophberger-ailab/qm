# Rendering

The `Render` button in the top bar opens the render panel. It runs the same flow as the `qm render` command, so both produce identical output (see [the command line](commands.md)).

## The matrix

A render is chosen by three things:

- **Topics**: what to render. One checkbox per topic, taken from the `topics` in the project's `qm.yml`. Without a `qm.yml`, every top-level content folder is a topic.
- **Audiences**: for whom. A topic with two or more audiences shows a checkbox for each below the topic. A topic with only one audience shows none and uses that one. Only the audiences a topic declares are offered.
- **Formats**: how. One checkbox per format, in the `Formats` group, taken from the `formats` in `qm.yml` (a website, a Word document, a PDF, or both). Without a `qm.yml` there is one: `web`.

Each checked topic is rendered once for every checked audience and every checked format. The audience boxes of a topic are dimmed while the topic itself is unchecked, but you can still change them in advance. A topic you have never set up starts with all its audiences checked.

A website format is the exception to "once per topic": it covers all checked topics in one site, and writes one site for each checked audience. A topic that takes no part in a checked format is skipped.

Your selection is remembered for each project in the [settings file](settings.md).

If `qm.yml` cannot be read, the panel shows the error instead of the matrix. Fix the file and reopen the panel.

## Running

1. Check what you want.
2. Press `Render` at the bottom of the panel.

The render runs in the background. The panel shows the log and updates it every second while it runs. At the end it says `Done` or `Render failed`.

- Only one render runs at a time.
- The log follows the newest line. Scroll up to stop following and hold your place; scroll back to the bottom or start a new render to follow again.

## Related

- Where the output goes is decided by `qm.yml`: by default below `_output`, in a folder per format. See the project README for how a project is set up.
- Content can be marked for a format or an audience; a render keeps only what the selection shows.
- Pages marked as drafts are not part of what a render publishes (see [the page tree](tree.md)).

Back to [Contents](index.md).
