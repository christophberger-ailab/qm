// Quarto Manager - app behavior (plain ES6, no build step)

// Open field
//
// The field shows the last element of the open project's path: the folder
// name is what tells one project from another, and the whole path only
// makes the field too narrow to read. The full path rides along in
// data-full and is what the submit sends, so the short label still opens
// the right project. Typing or pasting drops the full path, which is what
// lets a pasted path be opened as written.

function openPathInput() {
  return document.getElementById('open-path');
}

// showRecent opens or closes the dropdown of recently opened projects.
function showRecent(on) {
  var menu = document.getElementById('open-recent');
  var toggle = document.getElementById('open-recent-toggle');
  if (menu) {
    menu.hidden = !on;
  }
  if (toggle) {
    toggle.setAttribute('aria-expanded', on ? 'true' : 'false');
  }
}

// pickRecent puts a chosen project into the field -- short label, full
// path, full path as the tooltip -- and opens it.
function pickRecent(path, label) {
  var input = openPathInput();
  if (input) {
    input.value = label;
    input.dataset.full = path;
    input.title = path;
  }
  showRecent(false);
  htmx.ajax('POST', '/open', {
    target: '#main',
    swap: 'innerHTML',
    values: { path: path }
  });
}

document.body.addEventListener('click', function (evt) {
  var toggle = evt.target.closest('#open-recent-toggle');
  if (toggle) {
    var menu = document.getElementById('open-recent');
    showRecent(!!menu && menu.hidden);
    return;
  }
  var entry = evt.target.closest('.path-recent');
  if (entry) {
    pickRecent(entry.dataset.path, entry.textContent.trim());
    return;
  }
  // A click anywhere else closes the dropdown, the way a menu closes.
  if (!evt.target.closest('.path-field')) {
    showRecent(false);
  }
});

// A field the user has typed or pasted into no longer stands for the
// project it was showing, so the full path behind it is dropped and the
// text itself is what gets opened.
document.body.addEventListener('input', function (evt) {
  if (evt.target.id === 'open-path') {
    evt.target.dataset.full = '';
    evt.target.title = evt.target.value;
  }
});

var sortableInstances = [];

// Collapsed branches, keyed by each node's data-key. #tree is fully
// re-rendered on every move/create/delete/save, so this module-level state
// is what keeps collapsed branches collapsed across those re-renders. It is
// mirrored into localStorage (see load/saveCollapsed) so the expand/collapse
// state also survives a full page reload and future sessions.
var collapsed = new Set();

var COLLAPSED_KEY = 'collapsedKeys';

// loadCollapsed seeds the in-memory set from localStorage on startup.
function loadCollapsed() {
  try {
    var raw = localStorage.getItem(COLLAPSED_KEY);
    if (raw) {
      JSON.parse(raw).forEach(function (key) {
        collapsed.add(key);
      });
    }
  } catch (e) {
    // Ignore malformed or unavailable storage; start from an empty set.
  }
}

// saveCollapsed persists the current set after every change to it.
function saveCollapsed() {
  try {
    localStorage.setItem(COLLAPSED_KEY, JSON.stringify(Array.from(collapsed)));
  } catch (e) {
    // Ignore storage quota/availability errors; in-memory state still works.
  }
}

// applyCollapsed re-applies the persisted collapsed state to the freshly
// rendered tree.
function applyCollapsed(tree) {
  tree.querySelectorAll('li.page.has-children').forEach(function (li) {
    li.classList.toggle('collapsed', collapsed.has(li.dataset.key));
  });
}

function initTree() {
  // Destroy any stale Sortable instances before re-initializing.
  sortableInstances.forEach(function (inst) {
    inst.destroy();
  });
  sortableInstances = [];

  var tree = document.getElementById('tree');
  if (!tree) {
    return;
  }

  applyCollapsed(tree);

  var lists = tree.querySelectorAll('ul.children');
  lists.forEach(function (list) {
    var inst = Sortable.create(list, {
      group: 'pages',
      handle: '.drag-handle',
      animation: 150,
      fallbackOnBody: true,
      swapThreshold: 0.65,
      // Inverted swap makes the outer band of a row insert next to it, so
      // hovering the lower edge of the last subentry (in the gutter left of
      // its child list) inserts AFTER it — the "1.2" drop position that a
      // plain swap zone never offers with nested lists.
      invertSwap: true,
      invertedSwapThreshold: 0.65,
      // Only treat a list as an empty drop target when the pointer is right
      // inside it, so the empty child list under the last row does not grab
      // drops meant for the parent list's bottom strip.
      emptyInsertThreshold: 3,
      ghostClass: 'drag-ghost',
      onEnd: function (evt) {
        var sameList = evt.from === evt.to;
        var sameIndex = evt.oldIndex === evt.newIndex;
        if (sameList && sameIndex) {
          return;
        }

        var src = evt.item.dataset.path;
        var parent = evt.to.dataset.parent;
        var pos = evt.newIndex;

        // The page the editor has open travels with the move: moving into
        // another book renames files, and if the moved page is the open one
        // -- or sits inside a moved section -- the server answers with the
        // path the editor has to autosave to from now on.
        htmx.ajax('POST', '/move', {
          target: '#tree',
          swap: 'innerHTML',
          values: { src: src, parent: parent, pos: pos, open: currentPath || '' }
        });
      }
    });
    sortableInstances.push(inst);
  });
}

// initDivider makes a vertical divider draggable: dragging it resizes the
// pane next to it, which sits on the divider's left ('left') or right
// ('right') side. The chosen width is kept in localStorage under key so it
// survives reloads and the re-renders that replace the pane.
function initDivider(dividerID, paneID, key, side) {
  var divider = document.getElementById(dividerID);
  var pane = document.getElementById(paneID);
  if (!divider || !pane) {
    return;
  }

  var saved = localStorage.getItem(key);
  if (saved) {
    pane.style.width = saved;
  }

  divider.addEventListener('pointerdown', function (evt) {
    evt.preventDefault();
    divider.setPointerCapture(evt.pointerId);
    divider.classList.add('dragging');

    function onMove(e) {
      var panes = pane.parentElement.getBoundingClientRect();
      // Keep the pane on the other side usable; the resized pane's CSS
      // min-width provides the lower bound.
      var raw = side === 'right' ? panes.right - e.clientX : e.clientX - panes.left;
      var width = Math.max(Math.min(raw, panes.width - 200), 0);
      pane.style.width = width + 'px';
      refreshEditor(); // CodeMirror measures its own width
    }

    function onUp() {
      divider.removeEventListener('pointermove', onMove);
      divider.removeEventListener('pointerup', onUp);
      divider.classList.remove('dragging');
      localStorage.setItem(key, pane.style.width);
    }

    divider.addEventListener('pointermove', onMove);
    divider.addEventListener('pointerup', onUp);
  });
}

// Markdown preview
//
// The preview lives to the right of the editor and is rendered in the
// browser from the textarea's text (see preview.js), so it follows typing
// without a round trip. Whether it is open is kept in localStorage: the
// editor pane is re-rendered on every page switch, and the choice should
// outlive that -- and the session.

var PREVIEW_KEY = 'previewOpen';

var previewOpen = localStorage.getItem(PREVIEW_KEY) !== 'closed';

var previewTimer = null;

// applyPreview brings the pane and the toggle button in line with
// previewOpen. It runs after every swap that replaces the editor, both on
// afterSwap (so nothing flashes) and on afterSettle (which restores the
// swapped-in button's attributes, including aria-pressed).
function applyPreview() {
  var pane = document.getElementById('content-pane');
  if (pane) {
    pane.classList.toggle('preview-off', !previewOpen);
  }
  var button = document.getElementById('preview-toggle');
  if (button) {
    button.setAttribute('aria-pressed', previewOpen ? 'true' : 'false');
  }
  if (previewOpen) {
    updatePreview();
  }
}

// updatePreview re-renders the preview from what the editor currently holds.
// The page's path travels with the render: the preview resolves the page's
// image paths against it. It is read from the form rather than from
// currentPath so that it always describes the editor actually on screen.
function updatePreview() {
  var preview = document.getElementById('preview');
  var editor = document.querySelector('#content textarea.file-content');
  if (preview && editor) {
    var path = document.querySelector('#content input[name="path"]');
    renderPreview(preview, editor.value, path ? path.value : '');
  }
}

// schedulePreview coalesces the keystrokes of fast typing into one render.
function schedulePreview() {
  if (!previewOpen) {
    return;
  }
  clearTimeout(previewTimer);
  previewTimer = setTimeout(updatePreview, 150);
}

document.body.addEventListener('input', function (evt) {
  if (evt.target.classList.contains('file-content')) {
    schedulePreview();
  }
  if (evt.target.id === 'search-input') {
    // The field asks for its own hits; this only remembers the query.
    localStorage.setItem(SEARCH_KEY, evt.target.value);
  }
});

// The stylesheet dropdown only shows up above the preview when more than
// one custom CSS file exists (see the "content" template). Switching it
// swaps the document-wide <link> to the chosen file, remembers the choice
// server-side so it survives a restart and a fresh /open, and re-renders
// the preview so it reflects the new styles immediately.
document.body.addEventListener('change', function (evt) {
  if (evt.target.id !== 'preview-css-select') {
    return;
  }
  var name = evt.target.value;
  var link = document.getElementById('preview-css-link');
  if (link) {
    link.href = '/config/preview.css?file=' + encodeURIComponent(name) + '&v=' + Date.now();
  }
  fetch('/config/active-css', {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: 'file=' + encodeURIComponent(name)
  });
  updatePreview();
});

// currentPath is the page open in the editor; applySelection re-highlights
// it after every tree re-render (moves, saves, reloads).
var currentPath = null;

// syncCurrentPath reads the path out of the editor pane now on screen. The
// pane arrives either from a click on a tree entry or, when the app page is
// loaded, already filled with the page the project last had open; both must
// leave the same entry selected in the tree.
function syncCurrentPath() {
  var input = document.querySelector('#content input[name="path"]');
  currentPath = input ? input.value : null;
}

function applySelection() {
  document.querySelectorAll('#tree li.page').forEach(function (li) {
    li.classList.toggle('selected', !!currentPath && li.dataset.path === currentPath);
  });
}

// revealSelection expands the collapsed branches above the selected page,
// so a restored page is not hidden inside one.
function revealSelection() {
  var li = currentPath && document.querySelector('#tree li.page.selected');
  if (!li) {
    return;
  }
  for (var node = li.parentElement; node; node = node.parentElement) {
    if (node.id === 'tree') {
      break;
    }
    if (node.classList && node.classList.contains('collapsed')) {
      node.classList.remove('collapsed');
      collapsed.delete(node.dataset.key);
      saveCollapsed();
    }
  }
}

// Search
//
// The search field in the top bar is answered by /search with the pages
// that match, as data rather than as a result list: the tree already shows
// every page of the project, so the hits are shown by highlighting the
// entries that have them. That is also what makes a result outlast
// everything that re-renders the tree -- the two-second watch, moves,
// saves, page switches -- because the highlighting is re-applied from here
// after every swap, the way the selection and the collapsed branches are.

var SEARCH_KEY = 'searchQuery';

// searchHits maps a page's path to its number of hits, for the query the
// field currently holds.
var searchHits = new Map();

var searchRetry = null;

function searchQuery() {
  var input = document.getElementById('search-input');
  return input ? input.value : '';
}

// searchQuery is the query as the user wrote it; parseQuery below is what
// it means. The server reads it again for itself -- it has to, the index is
// there -- and the two readings have to agree, or the editor would paint
// something else than the tree counted. search.go holds the same rules in
// the same order, and its tests are the ones that pin them down.

// SEPARATORS is what stands between words: whitespace, punctuation of any
// script, and every ASCII character that is not a letter or a digit --
// Markdown and YAML are written entirely in those, so their syntax never
// ends up inside a word. Everything else belongs to one, emoji included.
var SEPARATORS = '\\s\\p{P}\\x00-\\x2F\\x3A-\\x40\\x5B-\\x60\\x7B-\\x7F';
var WORD_CHAR = '[^' + SEPARATORS + ']';
var NON_WORD_CHAR = '[' + SEPARATORS + ']';

// JOINER is what holds a compound word together. A single one of these
// between two word characters is part of the word -- "semi-wide" is one
// word -- while a run of them is the Markdown that it looks like.
var JOINER = '[-\\u2010\\u2011]';

var wordChar = new RegExp('^' + WORD_CHAR + '$', 'u');
var wordRun = new RegExp(WORD_CHAR + '+(?:' + JOINER + WORD_CHAR + '+)*', 'gu');

// QUOTE_ENDS pairs every quotation mark that can open a phrase with the one
// that closes it. The typographic closers are deliberately not openers, so
// that the apostrophe of "don’t" cannot start a phrase.
var QUOTE_ENDS = { '"': '"', "'": "'", '\u201C': '\u201D', '\u2018': '\u2019' };

// searchWords splits text into words the way the index splits page text.
function searchWords(text) {
  return text.toLowerCase().match(wordRun) || [];
}

// parseQuery reads a query into the terms it asks for: {words, exact}.
// Words between a pair of quotes are one term whose words have to stand
// together, everything outside is one term per word, and a closing quote
// also says the phrase is finished, which makes its last word exact.
function parseQuery(q) {
  var terms = [];
  // By code point, so that an emoji is one character and not two.
  var chars = Array.from(q);
  var loose = 0;
  for (var i = 0; i < chars.length; i++) {
    var end = QUOTE_ENDS[chars[i]];
    // A quote only quotes where a phrase can begin -- inside a word it is
    // an apostrophe.
    if (!end || (i > 0 && wordChar.test(chars[i - 1]))) {
      continue;
    }
    var close = chars.length; // an unclosed quote quotes the rest
    var closed = false;
    for (var j = i + 1; j < chars.length; j++) {
      if (chars[j] === end && (j + 1 === chars.length || !wordChar.test(chars[j + 1]))) {
        close = j;
        closed = true;
        break;
      }
    }
    terms = terms.concat(looseTerms(chars.slice(loose, i).join('')));
    terms.push({ words: searchWords(chars.slice(i + 1, close).join('')), exact: closed });
    i = close;
    loose = Math.min(close + 1, chars.length);
  }
  terms = terms.concat(looseTerms(chars.slice(loose).join('')));
  return terms.filter(function (term) {
    return !skipTerm(term);
  });
}

// looseTerms are the unquoted words of a query: one term each, every one of
// them matching by prefix.
function looseTerms(text) {
  return searchWords(text).map(function (word) {
    return { words: [word], exact: false };
  });
}

// skipTerm drops the terms the server drops: empty quotes, and the single
// letter or digit that starts almost every page and would light the whole
// tree up after the first keystroke. Any other single character -- an
// emoji, a symbol -- is rare enough to be exactly what was meant.
function skipTerm(term) {
  if (!term.words.length) {
    return true;
  }
  if (term.words.length > 1) {
    return false;
  }
  var chars = Array.from(term.words[0]);
  return chars.length === 1 && /[\p{L}\p{N}]/u.test(chars[0]);
}

// runSearch asks for the hits of whatever the field holds. The field posts
// itself as it is typed in; this is for the times the field did not change
// but the answer did -- the query restored on startup, and the retry while
// the index is still being built.
function runSearch() {
  htmx.ajax('GET', '/search?q=' + encodeURIComponent(searchQuery()), {
    target: '#search-results',
    swap: 'innerHTML'
  });
}

// readHits takes an answer apart and applies it to the tree and to the
// editor.
function readHits() {
  searchHits = new Map();
  document.querySelectorAll('#search-results .search-hits li').forEach(function (li) {
    searchHits.set(li.dataset.path, Number(li.dataset.count));
  });
  applyHits();
  setSearchTerms(parseQuery(searchQuery()));

  // The index is rebuilt in the background whenever the project changed on
  // disk. Until the new one is in, the answer describes the project as it
  // was a moment ago, and is worth asking for again.
  clearTimeout(searchRetry);
  if (document.querySelector('#search-results [data-indexing]')) {
    searchRetry = setTimeout(runSearch, 400);
  }
}

// applyHits marks the pages that matched, each with its number of hits. A
// branch holding hits is shown open for as long as they are there -- a hit
// inside a collapsed branch would be invisible -- but the remembered
// collapsed state is left alone, so clearing the search returns the tree to
// the shape the user gave it.
function applyHits() {
  var tree = document.getElementById('tree');
  if (!tree) {
    return;
  }
  tree.querySelectorAll('li.page').forEach(function (li) {
    li.classList.remove('hit', 'hit-branch');
    var row = li.querySelector(':scope > .row');
    if (row) {
      row.removeAttribute('data-hits');
    }
  });
  tree.querySelectorAll('li.page').forEach(function (li) {
    var count = searchHits.get(li.dataset.path);
    if (!count) {
      return;
    }
    li.classList.add('hit');
    var row = li.querySelector(':scope > .row');
    if (row) {
      row.dataset.hits = count;
    }
    for (var node = li.parentElement; node && node.id !== 'tree'; node = node.parentElement) {
      if (node.classList.contains('page')) {
        node.classList.add('hit-branch');
      }
    }
  });
}

// refreshHits re-applies the current search to a tree that has just been
// rendered again, and asks for the hits anew: what the tree shows changed,
// which is why it was rendered again, so the counts may have changed too.
function refreshHits() {
  applyHits();
  if (searchQuery()) {
    runSearch();
  }
}

// initSearch puts the last query back into the field and runs it. Nothing
// re-renders the field, but a reload starts it empty, and a search the user
// never cleared is one they are still working with.
function initSearch() {
  var input = document.getElementById('search-input');
  if (!input) {
    return;
  }
  input.value = localStorage.getItem(SEARCH_KEY) || '';
  if (input.value) {
    runSearch();
  }
}

// Render log
//
// The log panel polls /render/status every second and htmx replaces the
// whole <pre> with the log as it now stands, so the element the user was
// looking at is gone after every poll and the fresh one starts scrolled to
// the top. The panel therefore follows its newest line by itself. It stops
// following as soon as the user scrolls up -- reading the middle of a long
// log is why anyone scrolls up -- and follows again once they scroll back
// to the bottom, or start another render.

var renderLogFollow = true;

// renderLogTop is where the log stood just before the last swap, so a
// panel the user scrolled up in can be put back where they left it.
var renderLogTop = 0;

function renderLogOutput() {
  return document.querySelector('#render-log .render-output');
}

// atBottom allows a pixel of slack: fractional line heights keep scrollTop
// from ever reaching scrollHeight - clientHeight exactly.
function atBottom(el) {
  return el.scrollHeight - el.scrollTop - el.clientHeight <= 2;
}

// Scroll events do not bubble, and the element they come from is replaced
// every second, so the log is listened to on the way down instead.
document.addEventListener('scroll', function (evt) {
  var el = evt.target;
  if (el && el.classList && el.classList.contains('render-output')) {
    renderLogFollow = atBottom(el);
  }
}, true);

document.body.addEventListener('htmx:beforeSwap', function (evt) {
  var id = evt.detail && evt.detail.target && evt.detail.target.id;
  if (id === 'render-log') {
    var out = renderLogOutput();
    renderLogTop = out ? out.scrollTop : 0;
  }
});

document.addEventListener('DOMContentLoaded', function () {
  loadCollapsed();
  initTree();
  initDivider('divider', 'tree-pane', 'treePaneWidth', 'left');
  // The editor pane comes with the page when a project has a page to
  // restore, so it needs the same setup a swapped-in one gets.
  syncCurrentPath();
  applySelection();
  revealSelection();
  initDivider('preview-divider', 'preview', 'previewWidth', 'right');
  initEditor();
  applyPreview();
  refreshEditor();
  initSearch();
});

document.body.addEventListener('htmx:afterSwap', function (evt) {
  var id = evt.detail && evt.detail.target && evt.detail.target.id;
  if (id === 'main') {
    // /open replaces both panes at once: the tree of the project just
    // opened, and the editor holding the page that project last had open.
    syncCurrentPath();
  }
  if (id === 'tree' || id === 'main') { // /open swaps #main, everything else #tree
    initTree();
    applySelection();
    refreshHits(); // the tree was rebuilt; the current search still stands
  }
  if (id === 'main') {
    revealSelection();
  }
  if (id === 'search-results') {
    readHits();
  }
  if (id === 'content') { // track whatever the editor now shows
    syncCurrentPath();
    applySelection();
    // A fresh pane holds the page as it is on disk, so whatever the last
    // one failed to save is no longer this editor's problem.
    saveFailed = false;
    rebaseNotice = '';
    setSaveStatus('');
    showOverwrite(false);
  }
  if (id === 'content' || id === 'main') {
    initEditor(); // mount before the preview reads the editor's text
    applyPreview();
  }
  if (id === 'render-log') {
    var out = renderLogOutput();
    if (out) {
      out.scrollTop = renderLogFollow ? out.scrollHeight : renderLogTop;
    }
  }
});

// The top-bar "＋ Page" form inserts the new page after the one selected in
// the tree; carry that selection along as the "after" parameter.
document.body.addEventListener('htmx:configRequest', function (evt) {
  var elt = evt.detail && evt.detail.elt;
  if (elt && elt.classList && elt.classList.contains('new-file-form')) {
    evt.detail.parameters.after = currentPath || '';
  }
  // The Open field posts the label it shows; the project it stands for is
  // the full path behind it, which is what the server has to be given.
  if (elt && elt.classList && elt.classList.contains('open-form')) {
    var input = openPathInput();
    if (input && input.dataset.full) {
      evt.detail.parameters.path = input.dataset.full;
    }
  }
});

// /open replaces the divider along with the panes. Re-init only after htmx
// has settled: settling restores the swapped-in attributes of elements whose
// id survived the swap, which would wipe a width set during afterSwap.
document.body.addEventListener('htmx:afterSettle', function (evt) {
  var id = evt.detail && evt.detail.target && evt.detail.target.id;
  if (id === 'main') {
    initDivider('divider', 'tree-pane', 'treePaneWidth', 'left');
  }
  if (id === 'content' || id === 'main') { // the editor/preview split comes with the editor
    initDivider('preview-divider', 'preview', 'previewWidth', 'right');
    applyPreview();
    refreshEditor(); // the saved pane width has just been applied
  }
});

// Out-of-band swaps (e.g. /save refreshing #tree alongside #content) fire
// htmx:oobAfterSwap instead of htmx:afterSwap, so Sortable needs its own hook.
document.body.addEventListener('htmx:oobAfterSwap', function (evt) {
  var id = evt.detail && evt.detail.target && evt.detail.target.id;
  if (id === 'tree') {
    initTree();
    applySelection();
    refreshHits();
  }
  if (id === 'content-path') {
    // A move renamed the open page's file; the editor now posts to the new
    // path, so the tree entry to keep selected is the new one too.
    syncCurrentPath();
    applySelection();
    // An autosave that the rename had already broken is retried at the new
    // path right away -- its text exists nowhere else. A save that was
    // fine is left alone: re-posting it would put the editor's text, which
    // still carries the frontmatter from before the move, back over what
    // the move just wrote.
    if (saveFailed) {
      saveNow();
    }
  }
});

document.body.addEventListener('click', function (evt) {
  // A new render starts a new log; follow it, whatever the last one was
  // left scrolled to. htmx posts the button itself, so this only marks it.
  if (evt.target.closest('#render-run')) {
    renderLogFollow = true;
  }

  // Overwrite: the user answering a refused save with "mine wins".
  if (evt.target.closest('#save-overwrite')) {
    showOverwrite(false);
    saveNow(true);
    return;
  }

  // Preview toggle: open or close the preview beside the editor.
  if (evt.target.closest('#preview-toggle')) {
    previewOpen = !previewOpen;
    localStorage.setItem(PREVIEW_KEY, previewOpen ? 'open' : 'closed');
    applyPreview();
    refreshEditor(); // the editor just gained or lost half the pane
    return;
  }

  // Vim toggle: switch the editor's keymap.
  if (evt.target.closest('#vim-toggle')) {
    toggleVim();
    return;
  }

  // Expand all: forget every collapsed branch and reveal each subtree.
  if (evt.target.closest('#expand-all')) {
    collapsed.clear();
    document.querySelectorAll('#tree li.page.has-children').forEach(function (li) {
      li.classList.remove('collapsed');
    });
    saveCollapsed();
    return;
  }

  // Collapse all: remember every branch as collapsed and hide each subtree.
  if (evt.target.closest('#collapse-all')) {
    document.querySelectorAll('#tree li.page.has-children').forEach(function (li) {
      collapsed.add(li.dataset.key);
      li.classList.add('collapsed');
    });
    saveCollapsed();
    return;
  }

  var toggle = evt.target.closest('.toggle');
  if (toggle) {
    var node = toggle.closest('li.page');
    if (node && node.classList.contains('has-children')) {
      var key = node.dataset.key;
      if (collapsed.has(key)) {
        collapsed.delete(key);
      } else {
        collapsed.add(key);
      }
      node.classList.toggle('collapsed', collapsed.has(key));
      saveCollapsed();
    }
    return;
  }

  var link = evt.target.closest('a.title');
  if (!link) {
    return;
  }
  var item = link.closest('li');
  currentPath = item ? item.dataset.path : null;
  applySelection();
});

// Autosave feedback: the edit form posts /save with hx-swap="none", so the
// only visible trace is the status text next to the heading.
//
// A failed autosave is the one status that must not read as a detail: the
// text in the editor is then the only copy of the edit, and it is thrown
// away with the pane as soon as another page is opened. So a failure is
// shown as an error rather than as a note, and is remembered in saveFailed,
// which is what guards leaving the page below.
var saveFailed = false;

// rebaseNotice is what the last save had to put back: the server sends it
// as a qm:rebased event, which htmx fires before the request finishes, so
// it is held here until there is a "Saved" to say it alongside.
var rebaseNotice = '';

function setSaveStatus(text, kind) {
  var el = document.getElementById('save-status');
  if (el) {
    el.textContent = text;
    el.classList.toggle('failed', kind === 'failed');
    el.classList.toggle('notice', kind === 'notice');
  }
}

// showOverwrite offers, or withdraws, the way past a refused save.
function showOverwrite(on) {
  var button = document.getElementById('save-overwrite');
  if (button) {
    button.hidden = !on;
  }
}

// saveNow writes the editor's text without waiting out the edit form's
// one-second autosave delay. It is sourced from the form so that it reports
// through the same status as an ordinary autosave. force says the user
// answered a conflict with "mine wins".
function saveNow(force) {
  var form = document.querySelector('#content .edit-form');
  var path = document.getElementById('content-path');
  var area = document.querySelector('#content textarea.file-content');
  if (!form || !path || !area) {
    return;
  }
  var values = { path: path.value, body: area.value };
  if (force) {
    values.force = '1';
  }
  htmx.ajax('POST', '/save', { source: form, swap: 'none', values: values });
}

// The page the editor holds was written on top of the page as it stood when
// it opened, and the tree may have written to the same file since -- a move
// or a create renumbers a sibling group, and a move to another depth shifts
// the headings. The server replays the edit onto those rather than let it
// undo them, and says so here: the editor still shows the text from before,
// and only a reload brings it in line.
document.body.addEventListener('qm:rebased', function (evt) {
  var d = evt.detail || {};
  rebaseNotice = d.headings
    ? ' — the move\'s heading levels were kept; ↻ Reload to see them'
    : ' — the move\'s numbering was kept; ↻ Reload to see it';
});

document.body.addEventListener('htmx:beforeRequest', function (evt) {
  var elt = evt.detail && evt.detail.elt;
  if (elt && elt.classList && elt.classList.contains('edit-form')) {
    setSaveStatus('Saving…');
  }
});

document.body.addEventListener('htmx:afterRequest', function (evt) {
  var elt = evt.detail && evt.detail.elt;
  if (!elt || !elt.classList || !elt.classList.contains('edit-form')) {
    return;
  }
  var notice = rebaseNotice;
  rebaseNotice = '';
  saveFailed = !evt.detail.successful;
  if (!saveFailed) {
    showOverwrite(false);
    setSaveStatus('Saved' + notice, notice ? 'notice' : '');
    return;
  }
  var xhr = evt.detail.xhr;
  // A conflict is not a broken save but a decision to make: somebody else
  // wrote the file, and only the user can say whose version wins.
  if (xhr && xhr.status === 409) {
    showOverwrite(true);
    setSaveStatus('NOT SAVED — the file changed on disk since this page was opened. Reload to take that version, or Overwrite to keep yours.', 'failed');
    return;
  }
  var why = (xhr && (xhr.responseText || xhr.statusText) || '').trim();
  setSaveStatus('NOT SAVED' + (why ? ' — ' + why : '') + ' — your edits are only in this editor', 'failed');
});

// Opening another page, or reloading this one from disk, replaces the
// editor and with it the only copy of an edit that could not be saved. Ask
// first rather than drop it silently.
document.body.addEventListener('htmx:confirm', function (evt) {
  var elt = evt.detail && evt.detail.elt;
  var target = elt && elt.getAttribute && elt.getAttribute('hx-target');
  if (!saveFailed || target !== '#content') {
    return;
  }
  evt.preventDefault();
  if (window.confirm('This page has edits that could not be saved. Replacing the editor discards them. Continue?')) {
    evt.detail.issueRequest(true);
  }
});

// The same for a reload or a closed tab, which htmx never sees.
window.addEventListener('beforeunload', function (evt) {
  if (saveFailed) {
    evt.preventDefault();
    evt.returnValue = '';
  }
});
