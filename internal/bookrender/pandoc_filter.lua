-- The filter chain qm runs inside Pandoc (`qm: renderer: pandoc`).
--
-- qm prepends a `local QM = {...}` table: kind ("docx" or "pptx"), the
-- project's own filters, the callout icon directory and titles, and the
-- path the final document is captured to.
--
-- The chain, in order:
--   1. read the document metadata (every later pass needs it);
--   2. the project's filters, written for Quarto (the audience/content gate);
--   3. project-absolute image paths become project-relative;
--   4. page-break markers become the format's own page break;
--   5. callouts become what Quarto writes into a DOCX (or plain blocks);
--   6. DOCX only: capture the final document for qm's PDF layout.

-- The project's filters ask Quarto for metadata. Pandoc hands the metadata to
-- a filter only while it runs, so the first pass keeps it where this shim
-- finds it.
local META = nil
quarto = quarto or {}
quarto.metadata = quarto.metadata or {}
function quarto.metadata.get(path)
  local v = META
  for key in path:gmatch('[^.]+') do
    if v == nil then return nil end
    v = v[key]
  end
  return v
end

local function is_filter_name(k)
  return pandoc[k] ~= nil or k == 'Blocks' or k == 'Inlines' or k == 'Pandoc' or k == 'Meta'
end

-- Load a Quarto-style filter file in a scope of its own, so that several
-- filters defining `Div` do not overwrite one another.
local function load_filter(path)
  local env = setmetatable({}, { __index = _G })
  local chunk = assert(loadfile(path, 't', env))
  local returned = chunk()
  if returned ~= nil then return returned end
  local filter = {}
  for k, v in pairs(env) do
    if type(v) == 'function' and is_filter_name(k) then filter[k] = v end
  end
  return filter
end

local read_meta = { Pandoc = function(doc) META = doc.meta end }

-- Project-absolute paths (/assets/x.png) are relative to the project root,
-- which is where Pandoc runs.
local rebase = {
  Image = function(img)
    if img.src:sub(1, 1) == '/' and img.src:sub(2, 2) ~= '/' then
      img.src = img.src:sub(2)
      return img
    end
  end,
}

local pagebreak = {
  Div = function(div)
    if not div.classes:includes('qm-pagebreak') then return nil end
    if QM.kind == 'docx' then
      return pandoc.RawBlock('openxml', '<w:p><w:r><w:br w:type="page"/></w:r></w:p>')
    end
    return {} -- a slide deck has no pages to break
  end,
}

--------------------------------------------------------------------------
-- Callouts
--------------------------------------------------------------------------

-- Quarto's colors for its five callout types.
local COLORS = {
  note      = { border = '0758E5', fill = 'dae6fb' },
  tip       = { border = '00A047', fill = 'ccf1e3' },
  warning   = { border = 'EB9113', fill = 'fcefdc' },
  important = { border = 'CC1914', fill = 'f7dddc' },
  caution   = { border = 'FC5300', fill = 'ffe5d0' },
}

local function callout_kind(div)
  for _, c in ipairs(div.classes) do
    local t = c:match('^callout%-(%a+)$')
    if t and COLORS[t] then return t end
  end
  return nil
end

local function is_callout(div)
  return div.classes:includes('callout') or callout_kind(div) ~= nil
end

local function spacing(before, after, center)
  local s = '<w:pPr><w:spacing'
  if before then s = s .. ' w:before="' .. before .. '"' end
  if after then s = s .. ' w:after="' .. after .. '"' end
  s = s .. '/>' .. (center and '<w:textAlignment w:val="center"/>' or '') .. '</w:pPr>'
  return pandoc.RawInline('openxml', s)
end

local function title_inlines(title)
  local parsed = pandoc.read(title, 'markdown').blocks[1]
  return parsed and parsed.content or pandoc.Inlines({ pandoc.Str(title) })
end

-- The title of a callout, as inlines: its `title` attribute, else its first
-- heading (which then is the title and not part of the body), else the
-- default title of its type. nil for an untyped callout that has none.
local function callout_title(div, kind, blocks)
  local title = div.attributes['title']
  if title ~= nil and title ~= '' then return title_inlines(title) end
  if #blocks > 0 and blocks[1].t == 'Header' then
    local inlines = blocks[1].content
    blocks:remove(1)
    return inlines
  end
  if kind then return title_inlines(QM.titles[kind]) end
  return nil
end

-- A callout as a table in a DOCX: a shaded title row with an icon, then the
-- body row; a callout without a type is a bare bar. The table is raw XML
-- spread over the Pandoc blocks it surrounds, which is what lets the body
-- stay ordinary Pandoc content.
local function docx_callout(div)
  local kind = callout_kind(div)
  local blocks = pandoc.List(div.content)
  local title = callout_title(div, kind, blocks)
  local out = pandoc.List()
  if kind == nil then
    out:insert(pandoc.RawBlock('openxml',
      '<w:tbl><w:tblPr><w:tblStyle w:val="Table"/><w:tblInd w:w="164" w:type="dxa"/>' ..
      '<w:tblBorders><w:left w:val="single" w:sz="24" w:space="0" w:color="909090"/></w:tblBorders>' ..
      '<w:tblCellMar><w:left w:w="0" w:type="dxa"/><w:right w:w="0" w:type="dxa"/></w:tblCellMar>' ..
      '<w:tblLook w:firstRow="0" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="0" w:noVBand="0" w:val="0000"/>' ..
      '</w:tblPr><w:tblGrid><w:gridCol w:w="7920"/></w:tblGrid><w:tr><w:trPr><w:cantSplit/></w:trPr><w:tc>'))
    out:insert(pandoc.RawBlock('openxml',
      '<w:tcPr><w:tcMar><w:left w:w="144" w:type="dxa"/></w:tcMar></w:tcPr>'))
    out:insert(pandoc.Para({ spacing(16, 64), pandoc.Strong(title or {}) }))
  else
    local c = COLORS[kind]
    out:insert(pandoc.RawBlock('openxml',
      '<w:tbl><w:tblPr><w:tblStyle w:val="Table"/>' ..
      '<w:tblLook w:firstRow="0" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="0" w:noVBand="0" w:val="0000"/>' ..
      '<w:tblBorders><w:top w:val="single" w:sz="4" w:space="0" w:color="' .. c.border .. '"/>' ..
      '<w:left w:val="single" w:sz="24" w:space="0" w:color="' .. c.border .. '"/>' ..
      '<w:bottom w:val="single" w:sz="4" w:space="0" w:color="' .. c.border .. '"/>' ..
      '<w:right w:val="single" w:sz="4" w:space="0" w:color="' .. c.border .. '"/></w:tblBorders>' ..
      '<w:tblCellMar><w:left w:w="144" w:type="dxa"/><w:right w:w="144" w:type="dxa"/></w:tblCellMar>' ..
      '</w:tblPr><w:tr><w:trPr><w:cantSplit/></w:trPr><w:tc><w:tcPr>' ..
      '<w:shd w:color="auto" w:fill="' .. c.fill .. '" w:val="clear"/>' ..
      '<w:tcMar><w:top w:w="92" w:type="dxa"/><w:bottom w:w="92" w:type="dxa"/></w:tcMar></w:tcPr>'))
    local inlines = pandoc.List({ spacing(0, 0, true) })
    if QM.icons then
      inlines:insert(pandoc.Image({}, QM.icons .. '/' .. kind .. '.png', '',
        pandoc.Attr('', {}, { { 'project-resolve-ignore', 'true' }, { 'width', '16.0' }, { 'height', '16.0' } })))
      inlines:insert(pandoc.Space())
      inlines:insert(pandoc.Space())
    end
    inlines:extend(title)
    out:insert(pandoc.Para(inlines))
    out:insert(pandoc.Div({ pandoc.RawBlock('openxml',
      '</w:tc></w:tr><w:tr><w:trPr><w:cantSplit/></w:trPr><w:tc><w:tcPr><w:tcMar>' ..
      '<w:top w:w="108" w:type="dxa"/><w:bottom w:w="108" w:type="dxa"/></w:tcMar></w:tcPr>') }))
  end
  -- The first paragraph of the body gets space above it, the last one below.
  local n = #blocks
  for i, b in ipairs(blocks) do
    if b.t == 'Para' and (i == 1 or i == n) then
      b.content:insert(1, spacing(i == 1 and 16 or nil, i == n and 16 or nil))
    end
    out:insert(b)
  end
  -- A titled callout ends with an empty paragraph; the bare bar does not.
  out:insert(pandoc.RawBlock('openxml',
    (kind and '<w:p/>' or '') .. '</w:tc></w:tr></w:tbl>'))
  return pandoc.Div(out, pandoc.Attr('', { 'docx-callout' }))
end

-- Any other format has no table to draw the box in: a block quote, headed by
-- the title in bold. (That is what Quarto does too, down to the plain Div.)
local function plain_callout(div)
  local kind = callout_kind(div)
  local blocks = pandoc.List(div.content)
  local title = callout_title(div, kind, blocks)
  local quote = pandoc.List()
  if title then
    quote:insert(pandoc.Para({ pandoc.Strong(title) }))
  end
  quote:extend(blocks)
  return pandoc.Div({ pandoc.BlockQuote(quote) })
end

local function is_docx_callout(b)
  return b and b.t == 'Div' and b.classes:includes('docx-callout')
end

local callouts = {
  Div = function(div)
    if not is_callout(div) then return nil end
    if QM.kind == 'docx' then return docx_callout(div) end
    return plain_callout(div)
  end,
  -- Word merges tables that touch, so a callout that follows a table or
  -- another callout gets an empty line before it. (Quarto does exactly this
  -- and no more: a table that follows a callout is left touching it.)
  Blocks = function(blocks)
    if QM.kind ~= 'docx' then return nil end
    local out, changed = pandoc.List(), false
    for _, b in ipairs(blocks) do
      local prev = out[#out]
      if is_docx_callout(b) and prev and (prev.t == 'Table' or is_docx_callout(prev)) then
        out:insert(pandoc.Para({ pandoc.Str(' ') }))
        changed = true
      end
      out:insert(b)
    end
    if changed then return out end
  end,
}

-- A slide deck has no use for an empty paragraph, and Pandoc turns one into a
-- slide of its own. Gating an audience's image out of a paragraph that held
-- nothing else is what leaves them behind. Nor does it want the block-level
-- HTML comments the page templates carry (`<!-- Speaker notes go here -->`):
-- Pandoc picks a slide's layout by what the slide holds, and a comment
-- counts. (Quarto removes both as well; a comment inside a line stays.)
local drop_empty = {
  Para = function(p)
    if QM.kind == 'pptx' and #p.content == 0 then return {} end
  end,
  RawBlock = function(r)
    if QM.kind == 'pptx' and r.format == 'html' and r.text:match('^%s*<!%-%-') then return {} end
  end,
}

-- qm lays out the PDF from the document as it is about to be written.
local capture = {
  Pandoc = function(doc)
    local f = assert(io.open(QM.capture, 'w'))
    f:write(pandoc.write(doc, 'json'))
    f:close()
  end,
}

local chain = { read_meta }
for _, path in ipairs(QM.filters) do chain[#chain + 1] = load_filter(path) end
chain[#chain + 1] = rebase
chain[#chain + 1] = pagebreak
chain[#chain + 1] = callouts
chain[#chain + 1] = drop_empty
if QM.capture then chain[#chain + 1] = capture end
return chain
