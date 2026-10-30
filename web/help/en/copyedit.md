# Copyediting with an LLM

The copyedit pane sends the page to a language model and shows its suggestions. Open it with the `Copyedit` button (see [the panes](panes.md)).

## Set it up first

You need at least one task and one connection. Both are on [config pages](settings.md) and are stored in your own settings file, not in the project.

**Editing tasks** (Config, then Copyedit: Editing tasks). A task is a title and a prompt, for example "Passive voice" with an instruction about what to look for. You can add, edit, and delete tasks. A task needs both a title and a prompt. The pane lists tasks by title, and the prompt is shown as a tooltip.

**API connections** (Config, then Copyedit: API connections). A connection has a name, an API kind, a model, and usually a base URL and a key. You can save any number of them.

- `Anthropic` posts to `<base URL>/messages`.
- `OpenAI-compatible` posts to `<base URL>/chat/completions`. This is what OpenAI, Ollama, LM Studio, and OpenRouter speak. A local model needs no key.
- `GitHub Copilot` runs the GitHub Copilot CLI. It must be installed, on your `PATH` (or named by `COPILOT_CLI_PATH`), and signed in. This kind has no base URL and no key. The CLI session gets no tools of its own, so a task run can only read the page it is sent; a chat gets only the read-only search tools described below.

You may paste the full endpoint as the base URL; the path is not added twice. A stored key is never sent back to the browser. Leave the key field empty when editing to keep it.

## Running tasks

- The `Model` dropdown at the top of the pane picks the connection. Your choice is remembered.
- Click a task to run it on the page.
- Or tick several tasks and press `Run selected`. The box `Select all` ticks all or none. Your ticks are remembered.

The text sent is what the editor holds, including unsaved edits.

## One call or one call per task

On the Config page for editing tasks, the mode setting decides how several ticked tasks run:

- `One call for all ticked tasks` (the default) sends the page once.
- `One call per ticked task` makes one call after another. The model can attend to each task fully, and the answers may read better.

Each list of suggestions says which way it was produced. If one task fails, the others are still shown, with the failure named.

## Working with suggestions

The pane switches from the task list to the suggestions, each with the quoted passage, what to put instead, and why. A run of several tasks is grouped by task. Chips above the list narrow it to one task.

- The passage of each suggestion is marked in the editor. Click a suggestion to scroll the editor to it.
- `Apply` writes the replacement into the page at the mark. It acts like typing, so autosave, the preview, and undo all work as usual.
- `Done` marks a suggestion you carried out by hand. Every suggestion has it. Suggestions with no replacement, or whose passage could not be found, offer only `Done`.
- A suggestion whose passage is not in the page is listed and says so.
- `◀ Tasks` returns to the task list and removes the marks.

Suggestions are kept for each page while you work on other pages, so you can come back to them. Going back to the task list or opening another project forgets them.

Errors, such as no connection or a refusal from the API, are shown in the pane.

## Chatting about the documentation

The box at the foot of the pane lets you ask the selected model about the documentation: what it says, where a topic is covered, whether two pages agree. Press Enter to send, Shift+Enter for a new line.

- To answer, the model can search the pages, list them, and read them. Below each answer the pane says what it looked up. It reads the page open in the editor as the editor holds it, unsaved edits included.
- The model cannot change any page. When a change to the open page would help, it suggests one, and the suggestion is listed below its answer exactly like a task's suggestion: marked in the editor, with `Apply` and `Done`. Changes to other pages are described in the answer.
- Asked from the task list, the chat takes the list's place. Asked while a run's suggestions are shown, it continues below them, so you can ask about them.
- The conversation is kept with the page like the suggestions are, and `◀ Tasks` ends it.

Back to [Contents](index.md).
