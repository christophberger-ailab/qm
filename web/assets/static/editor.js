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
    extraKeys: { Enter: 'newlineAndIndentContinueMarkdownList', 'Ctrl-Space': 'autocomplete' },
    hintOptions: pathHintOptions
  });
  clearCompletionCache();
  cm.on('inputRead', maybeOpenPathCompletion);

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

// Path completion
//
// Typing inside a Markdown link's destination -- `[text](here` or
// `![alt](here`, whichever kind of bracket opened it -- offers the
// entries of the directory named so far, images for the first kind and
// `.qmd` pages for the second; a directory is offered either way, so the
// user can descend into it. The segment being typed matches anywhere in a
// name, not only at its start -- typing "alarmplan" finds
// "dispatcher_alarmplan.qmd" -- with a match at the start listed first.
// Up and Down move the selection, Tab or Enter inserts it, Esc closes the
// popup: CodeMirror's own show-hint addon supplies all four out of the
// box.
//
// A destination containing a character Markdown would otherwise read as
// ending it -- a space above all, but also the brackets and parentheses
// themselves -- is written the way CommonMark reads such a destination
// literally: wrapped in `<...>`. Picking a plain name keeps or restores
// the bare form; picking one with a space in it switches to the wrapped
// form, on the way in either direction, so the user never has to type the
// angle brackets by hand.

// completionCache holds the directory listings already fetched, so typing
// further characters of the same segment does not ask the server again.
// It is keyed by "kind|dir" and cleared whenever the project on disk may
// have changed under it.
var completionCache = {};

function clearCompletionCache() {
  completionCache = {};
}

// Any request that is not a plain autosave may have created, removed, or
// renamed a file -- /create, /delete, and /move all do -- so the safe rule
// is to drop the cache on every request rather than name each one.
document.body.addEventListener('htmx:afterRequest', clearCompletionCache);

// pathHintOptions is installed as the editor's `hintOptions`, which is
// what both the automatic popup and the manual Ctrl-Space command (CM's
// own "autocomplete" built-in) read.
var pathHintOptions = {
  hint: pathHint,
  // A single match is common while a name is still being typed -- most
  // directories have more than one entry starting the same way -- and
  // inserting it before the user chose to would take typing back out of
  // their hands.
  completeSingle: false,
  // The default also treats "(" and "<" as closing characters, which
  // would dismiss the popup on the very brackets a link destination
  // starts with. ")" is enough: it is what a finished destination, bare
  // or wrapped, always ends on.
  closeCharacters: /[)]/
};

// maybeOpenPathCompletion opens the popup on the next keystroke inside a
// link destination, unless one is already open -- show-hint keeps an open
// popup current on its own, through the hint function below being asked
// again on every cursor move.
function maybeOpenPathCompletion(instance) {
  if (instance.state.completionActive || !linkDestAt(instance)) {
    return;
  }
  instance.showHint();
}

// linkDestAt reports the link destination the cursor sits in, or null when
// it does not. Only the current line is considered: a link destination
// cannot contain a newline.
//
// `open` is the index of the destination's own "(", `angle` whether it
// opened with "(<", and `typed` the destination text between there and
// the cursor. `isImage` is read off the "!" before the link's own "[",
// found by walking backward with a bracket-depth counter so that an image
// nested inside a link's text -- `[![alt](img.png)](page/)` -- resolves
// against its own brackets rather than the enclosing ones.
function linkDestAt(cm) {
  var cur = cm.getCursor();
  var line = cm.getLine(cur.line);
  var ch = cur.ch;

  var open = -1;
  for (var i = Math.min(ch, line.length) - 1; i >= 1; i--) {
    if (line.charAt(i) === '(' && line.charAt(i - 1) === ']') {
      open = i;
      break;
    }
  }
  if (open < 0) {
    return null;
  }

  var angle = line.charAt(open + 1) === '<';
  var start = angle ? open + 2 : open + 1;
  if (start > ch) {
    return null;
  }
  // A destination already closed before the cursor -- by its own ">", or
  // by the whitespace or ")" that ends a bare one -- means the cursor has
  // moved on to the title or past the link entirely.
  for (var j = start; j < ch; j++) {
    var c = line.charAt(j);
    if (c === '\\') {
      j++;
      continue;
    }
    if (angle ? c === '>' : (c === ')' || c === ' ' || c === '\t')) {
      return null;
    }
  }

  return {
    open: open,
    angle: angle,
    typed: line.slice(start, ch),
    isImage: isImageBefore(line, open - 1)
  };
}

// isImageBefore reports whether the "]" at index p closes an image
// reference rather than a link, by walking back to its matching "[" and
// checking for a preceding "!".
function isImageBefore(line, p) {
  var depth = 0;
  for (var j = p - 1; j >= 0; j--) {
    var c = line.charAt(j);
    if (c === ']') {
      depth++;
    } else if (c === '[') {
      if (depth === 0) {
        return j > 0 && line.charAt(j - 1) === '!';
      }
      depth--;
    }
  }
  return false;
}

// splitTyped separates a destination typed so far into the directory
// named and the segment being completed -- the part after its last "/",
// or all of it when it names none.
function splitTyped(typed) {
  var i = typed.lastIndexOf('/');
  if (i < 0) {
    return { dir: '', segment: typed };
  }
  return { dir: typed.slice(0, i), segment: typed.slice(i + 1) };
}

// currentPageDir is the folder of the page open in the editor, which a
// relative destination resolves against -- the same rule preview.js
// resolves an image source by.
function currentPageDir() {
  var input = document.querySelector('#content input[name="path"]');
  var path = (input && input.value) || '';
  var cut = path.lastIndexOf('/');
  return cut < 0 ? '' : path.slice(0, cut);
}

// pathHint is the hint function CodeMirror calls, and keeps calling as
// the user keeps typing: it is asked again on every cursor move while the
// popup is open, which is what lets the same popup answer for a
// destination that grows, shrinks, or is completed a directory at a time.
function pathHint(cm, callback) {
  var cur = cm.getCursor();
  var dest = linkDestAt(cm);
  if (!dest) {
    callback(null);
    return;
  }

  var absolute = dest.typed.charAt(0) === '/';
  var rest = absolute ? dest.typed.slice(1) : dest.typed;
  var split = splitTyped(rest);
  var segStart = cur.ch - split.segment.length;
  var from = CodeMirror.Pos(cur.line, segStart);

  var pageDir = absolute ? '' : currentPageDir();
  var queryDir = normalizePath(pageDir ? (split.dir ? pageDir + '/' + split.dir : pageDir) : split.dir);
  var kind = dest.isImage ? 'image' : 'page';

  fetchCompletions(queryDir, kind).then(function (entries) {
    var needle = split.segment.toLowerCase();
    var matches = entries.filter(function (e) {
      return e.name.toLowerCase().indexOf(needle) >= 0;
    });
    if (!matches.length) {
      callback(null);
      return;
    }
    // A name starting with the segment is what the user most likely
    // means -- "images" typing "im" -- and sorts first; a name that only
    // contains it somewhere past its start -- "dispatcher_alarmplan"
    // typing "alarmplan" -- still matches, just after. Array.prototype.sort
    // is stable, so entries tied on that keep the order /complete answered
    // in: directories before files, then alphabetically.
    matches.sort(function (a, b) {
      return startsWithScore(a, needle) - startsWithScore(b, needle);
    });
    callback({
      list: matches.map(function (e) {
        return pathHintItem(e, dest, absolute, split.dir);
      }),
      from: from,
      to: CodeMirror.Pos(cur.line, cur.ch)
    });
  });
}

// startsWithScore ranks a prefix match before one that only matches
// further into the name.
function startsWithScore(entry, needle) {
  return entry.name.toLowerCase().indexOf(needle) === 0 ? 0 : 1;
}

// The hint function fetches over the network, so show-hint must wait for
// the callback rather than use its return value.
pathHint.async = true;

// pathHintItem builds one entry of the popup. Picking it inserts the
// destination typed so far with its last segment completed -- absolute or
// relative, matching however the user started -- wrapped in "<...>" when
// what that spells needs it and left bare otherwise. Picking a directory
// reopens the popup on its contents, so the next segment completes the
// same way.
function pathHintItem(entry, dest, absolute, dirSoFar) {
  var name = entry.name + (entry.dir ? '/' : '');
  var full = (absolute ? '/' : '') + (dirSoFar ? dirSoFar + '/' : '') + name;
  return {
    displayText: name,
    className: entry.dir ? 'cm-path-hint-dir' : null,
    hint: function (cm) {
      insertLinkDest(cm, dest, full);
      if (entry.dir) {
        // Reopening runs after show-hint's own close(), which follows
        // this callback synchronously; a fresh tick is what lets the new
        // popup list the directory just inserted rather than closing
        // right behind it.
        setTimeout(function () {
          cm.showHint();
        }, 0);
      }
    }
  };
}

// insertLinkDest replaces a link destination with the given text, wrapping
// it in CommonMark's "<...>" form when it contains a character that would
// otherwise end it early -- whitespace, or one of the brackets a bare
// destination cannot balance -- and leaving it bare otherwise. Either way
// the whole destination is replaced, so a bare destination can pick up the
// wrapping it needs, and a wrapped one can lose it again, as the pick
// warrants.
function insertLinkDest(cm, dest, text) {
  var cur = cm.getCursor();
  var line = cm.getLine(cur.line);
  var start = dest.angle ? dest.open + 2 : dest.open + 1;

  var end = start;
  for (; end < line.length; end++) {
    var c = line.charAt(end);
    if (c === '\\') {
      end++;
      continue;
    }
    if (dest.angle ? c === '>' : (c === ')' || c === ' ' || c === '\t')) {
      break;
    }
  }
  var consumesCloser = dest.angle && end < line.length && line.charAt(end) === '>';

  var needsAngle = /[\s<>()]/.test(text);
  var replacement = needsAngle ? '<' + text + '>' : text;

  cm.replaceRange(
    replacement,
    CodeMirror.Pos(cur.line, dest.open + 1),
    CodeMirror.Pos(cur.line, consumesCloser ? end + 1 : end)
  );
}

// fetchCompletions asks /complete for one directory's entries, caching the
// answer under its kind and path.
function fetchCompletions(dir, kind) {
  var key = kind + '|' + dir;
  var cached = completionCache[key];
  if (cached) {
    return cached;
  }
  var url = '/complete?dir=' + encodeURIComponent(dir || '.') + '&kind=' + kind;
  var promise = fetch(url)
    .then(function (resp) {
      return resp.ok ? resp.json() : { entries: [] };
    })
    .then(function (data) {
      return (data && data.entries) || [];
    })
    .catch(function () {
      return [];
    });
  completionCache[key] = promise;
  return promise;
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
