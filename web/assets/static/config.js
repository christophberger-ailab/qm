// Quarto Sorter - configuration page behavior (plain ES6, no build step)

// initPreviewCSSEditor mounts CodeMirror on the custom preview stylesheet
// textarea. The textarea remains the form field, so submitting the form
// keeps working even if CodeMirror failed to load.
function initPreviewCSSEditor() {
  var area = document.querySelector('textarea.preview-css-content');
  if (!area || typeof CodeMirror === 'undefined') {
    return;
  }

  var editor = CodeMirror.fromTextArea(area, {
    mode: 'css',
    lineWrapping: true
  });

  if (area.form) {
    area.form.addEventListener('submit', function () {
      editor.save();
    });
  }
}

document.addEventListener('DOMContentLoaded', initPreviewCSSEditor);
