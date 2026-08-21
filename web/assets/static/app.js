// Quarto Sorter - app behavior (plain ES6, no build step)

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

        htmx.ajax('POST', '/move', {
          target: '#tree',
          swap: 'innerHTML',
          values: { src: src, parent: parent, pos: pos }
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

// searchWords splits a query into terms the way the index splits page text,
// dropping the single letters the server drops, so the editor highlights
// exactly what the tree counted.
function searchWords(q) {
  return q.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter(function (word) {
    return word.length > 1;
  });
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
  setSearchTerms(searchWords(searchQuery()));

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
  }
  if (id === 'content' || id === 'main') {
    initEditor(); // mount before the preview reads the editor's text
    applyPreview();
  }
});

// The top-bar "＋ Page" form inserts the new page after the one selected in
// the tree; carry that selection along as the "after" parameter.
document.body.addEventListener('htmx:configRequest', function (evt) {
  var elt = evt.detail && evt.detail.elt;
  if (elt && elt.classList && elt.classList.contains('new-file-form')) {
    evt.detail.parameters.after = currentPath || '';
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
});

document.body.addEventListener('click', function (evt) {
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
function setSaveStatus(text) {
  var el = document.getElementById('save-status');
  if (el) {
    el.textContent = text;
  }
}

document.body.addEventListener('htmx:beforeRequest', function (evt) {
  var elt = evt.detail && evt.detail.elt;
  if (elt && elt.classList && elt.classList.contains('edit-form')) {
    setSaveStatus('Saving…');
  }
});

document.body.addEventListener('htmx:afterRequest', function (evt) {
  var elt = evt.detail && evt.detail.elt;
  if (elt && elt.classList && elt.classList.contains('edit-form')) {
    setSaveStatus(evt.detail.successful ? 'Saved' : 'Save failed');
  }
});
