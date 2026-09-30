-- Runs after Quarto's filters, in the same Pandoc invocation as the DOCX writer.
function Pandoc(doc)
  local path = os.getenv('QM_CAPTURE_AST')
  if not path or path == '' then error('QM_CAPTURE_AST is not set') end
  local file = assert(io.open(path, 'w'))
  assert(file:write(pandoc.write(doc, 'json')))
  assert(file:close())
end
