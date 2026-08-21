// Quarto Sorter - the Markdown editor (plain ES6, no build step)
//
// CodeMirror mounts on top of the textarea in the content template rather
// than replacing it: the textarea stays the field the edit form posts to
// /save, and every change is written back to it. Autosave and the preview
// therefore keep working through the wiring they already had.
//
// Vim keybindings are a per-user choice and off by default. CodeMirror is
// used either way -- that is what gives the editor its Markdown
// highlighting -- and the toggle only swaps the keymap.
//
// This file holds the editor itself; app.js owns the wiring, the same split
// preview.js and app.js already use.

var VIM_KEY = 'vimMode';

// vimOn is the remembered choice; cm is the editor mounted on the page
// currently open, or null when no page is.
var vimOn = localStorage.getItem(VIM_KEY) === 'on';
var cm = null;

// editorMode is Quarto Markdown as CodeMirror sees it: a YAML frontmatter
// block above a GitHub-flavoured Markdown body. Fenced code blocks are not
// highlighted by language -- that would mean vendoring a mode per language.
var editorMode = { name: 'yaml-frontmatter', base: 'gfm' };

// initEditor mounts CodeMirror on the textarea of the editor now on screen.
// The content pane is replaced on every page switch, so any previous
// instance is handed back to its textarea first.
function initEditor() {
  if (cm) {
    // The old editor may already have been swapped out of the page;
    // dropping it must not stop the new one from mounting.
    try {
      cm.toTextArea();
    } catch (e) {
      // Already gone with the swapped-out pane.
    }
    cm = null;
  }

  var area = document.querySelector('#content textarea.file-content');
  if (!area || typeof CodeMirror === 'undefined') {
    return; // no page open, or the asset is missing: the textarea does
  }

  cm = CodeMirror.fromTextArea(area, {
    mode: editorMode,
    lineWrapping: true,
    keyMap: vimOn ? 'vim' : 'default',
    extraKeys: { Enter: 'newlineAndIndentContinueMarkdownList' }
  });

  // The textarea is what /save posts and what the preview reads, so it has
  // to follow the editor. Re-emitting the input event is what keeps both
  // working: htmx autosaves on `input` from the textarea, and app.js
  // refreshes the preview from the same event.
  cm.on('change', function () {
    area.value = cm.getValue();
    area.dispatchEvent(new Event('input', { bubbles: true }));
  });

  applyVim();
  // A page opened while a search is running arrives with its hits already
  // marked, and scrolled to the first of them.
  applySearchHighlight();
  scrollToFirstHit();
}

// Search highlighting
//
// The project search highlights the pages that matched in the tree; the
// page open in the editor gets the same treatment inside its text. An
// overlay is CodeMirror's own way to paint on top of the syntax
// highlighting without touching the document, and "searching" is the token
// style its stylesheet already dresses as a hit.
//
// app.js owns the query; the editor is only told which words it matched.

var searchTerms = [];
var searchOverlay = null;

// setSearchTerms paints the given words, replacing whatever was painted
// before. An empty list clears the highlighting.
function setSearchTerms(terms) {
  searchTerms = terms;
  applySearchHighlight();
}

// searchPattern matches the words starting with any of the terms -- the
// rule the index matches by, so the editor marks what the tree counted.
function searchPattern() {
  if (!searchTerms.length) {
    return null;
  }
  var alternatives = searchTerms.map(function (term) {
    return term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  });
  return new RegExp(
    '(?<![\\p{L}\\p{N}])(?:' + alternatives.join('|') + ')[\\p{L}\\p{N}]*',
    'giu'
  );
}

function applySearchHighlight() {
  if (!cm) {
    return;
  }
  if (searchOverlay) {
    cm.removeOverlay(searchOverlay);
    searchOverlay = null;
  }
  var pattern = searchPattern();
  if (!pattern) {
    return;
  }
  // The token function is called with the stream parked at some position in
  // a line and must leave it further along. Searching from lastIndex rather
  // than from a slice of the line is what lets the pattern look at the
  // character before a hit, which is how it tells a word start apart from
  // the middle of a longer word.
  searchOverlay = {
    token: function (stream) {
      pattern.lastIndex = stream.pos;
      var match = pattern.exec(stream.string);
      if (match && match.index === stream.pos) {
        stream.pos += match[0].length;
        return 'searching';
      }
      stream.pos = match ? match.index : stream.string.length;
      return null;
    }
  };
  cm.addOverlay(searchOverlay);
}

// scrollToFirstHit brings the first hit into view when a page is opened
// from the tree, so a page found by the search does not open above it. It
// scrolls only: moving the cursor would take the editor away from the
// place the user is actually working in.
function scrollToFirstHit() {
  var pattern = searchPattern();
  if (!cm || !pattern) {
    return;
  }
  var cursor = cm.getSearchCursor(pattern, { line: 0, ch: 0 }, { multiline: false });
  if (cursor.findNext()) {
    cm.scrollIntoView({ from: cursor.from(), to: cursor.to() }, 80);
  }
}

// refreshEditor makes CodeMirror remeasure, which it needs whenever the
// pane it sits in changes width.
function refreshEditor() {
  if (cm) {
    cm.refresh();
  }
}

// applyVim brings the editor and the toggle button in line with vimOn. Like
// the preview toggle, the button is re-rendered with the editor, so its
// pressed state has to be restored after every swap.
function applyVim() {
  if (cm) {
    cm.setOption('keyMap', vimOn ? 'vim' : 'default');
  }
  var button = document.getElementById('vim-toggle');
  if (button) {
    button.setAttribute('aria-pressed', vimOn ? 'true' : 'false');
  }
}

// toggleVim flips vim mode and remembers the choice across sessions.
function toggleVim() {
  vimOn = !vimOn;
  localStorage.setItem(VIM_KEY, vimOn ? 'on' : 'off');
  applyVim();
  if (cm) {
    cm.focus();
  }
}

// `:w` writes now rather than waiting out the edit form's one-second
// autosave delay -- not waiting is the whole point of typing it. The
// request is sourced from the form so that it reports through the same
// "Saving…"/"Saved" status as an ordinary autosave.
if (typeof CodeMirror !== 'undefined' && CodeMirror.Vim) {
  CodeMirror.Vim.defineEx('write', 'w', function () {
    var path = document.querySelector('#content input[name="path"]');
    if (!cm || !path) {
      return;
    }
    htmx.ajax('POST', '/save', {
      source: '.edit-form',
      swap: 'none',
      values: { path: path.value, body: cm.getValue() }
    });
  });
}
