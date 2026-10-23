-- Written by `qm prepare`. Do not edit.
--
-- A `qm: renderer: qm` format profile declares this filter at Quarto's last
-- entry point, after all of Quarto's own filters and right before the DOCX
-- writer:
--
--   filters:
--     - path: _build/qm-capture.lua
--       at: post-finalize
--
-- It saves the filtered document for `qm finalize`, which lays out the PDF
-- from it. Only the DOCX render is captured.
function Pandoc(doc)
  if FORMAT ~= 'docx' then return nil end
  local path = pandoc.path.join({ quarto.project.directory, '_build', 'qm-document.json' })
  -- The one thing Quarto removes after this entry point: its empty, hidden
  -- file-metadata marker.
  local blocks = pandoc.Blocks({})
  for _, block in ipairs(doc.blocks) do
    if not (block.t == 'Div' and block.classes:includes('hidden') and #block.content == 0) then
      blocks:insert(block)
    end
  end
  local captured = pandoc.Pandoc(blocks, doc.meta)
  local file = assert(io.open(path, 'w'))
  assert(file:write(pandoc.write(captured, 'json')))
  assert(file:close())
end
