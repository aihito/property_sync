-- Minimal pure-Lua JSON encode/decode for property_sync 对拍.
-- Sufficient for mutate batches and sync views (no unicode escapes beyond \uXXXX).

local JSON = {}

local function skip_ws(s, i)
  local _, j = s:find("^[ \t\n\r]*", i)
  return (j or i - 1) + 1
end

local function parse_string(s, i)
  i = i + 1
  local out = {}
  while i <= #s do
    local c = s:sub(i, i)
    if c == '"' then
      return table.concat(out), i + 1
    elseif c == "\\" then
      local n = s:sub(i + 1, i + 1)
      local map = { b = "\b", f = "\f", n = "\n", r = "\r", t = "\t", ['"'] = '"', ["\\"] = "\\", ["/"] = "/" }
      if n == "u" then
        local hex = s:sub(i + 2, i + 5)
        out[#out + 1] = utf8.char(tonumber(hex, 16))
        i = i + 6
      else
        out[#out + 1] = map[n] or n
        i = i + 2
      end
    else
      out[#out + 1] = c
      i = i + 1
    end
  end
  error("unterminated string")
end

local parse_value

local function parse_array(s, i)
  i = i + 1
  local arr = {}
  i = skip_ws(s, i)
  if s:sub(i, i) == "]" then
    return arr, i + 1
  end
  while true do
    local v
    v, i = parse_value(s, i)
    arr[#arr + 1] = v
    i = skip_ws(s, i)
    local c = s:sub(i, i)
    if c == "]" then
      return arr, i + 1
    elseif c == "," then
      i = skip_ws(s, i + 1)
    else
      error("expected , or ] at " .. i)
    end
  end
end

local function parse_object(s, i)
  i = i + 1
  local obj = {}
  i = skip_ws(s, i)
  if s:sub(i, i) == "}" then
    return obj, i + 1
  end
  while true do
    if s:sub(i, i) ~= '"' then
      error("expected string key at " .. i)
    end
    local key
    key, i = parse_string(s, i)
    i = skip_ws(s, i)
    if s:sub(i, i) ~= ":" then
      error("expected : at " .. i)
    end
    i = skip_ws(s, i + 1)
    local val
    val, i = parse_value(s, i)
    obj[key] = val
    i = skip_ws(s, i)
    local c = s:sub(i, i)
    if c == "}" then
      return obj, i + 1
    elseif c == "," then
      i = skip_ws(s, i + 1)
    else
      error("expected , or } at " .. i)
    end
  end
end

parse_value = function(s, i)
  i = skip_ws(s, i)
  local c = s:sub(i, i)
  if c == '"' then
    return parse_string(s, i)
  elseif c == "{" then
    return parse_object(s, i)
  elseif c == "[" then
    return parse_array(s, i)
  elseif c == "t" and s:sub(i, i + 3) == "true" then
    return true, i + 4
  elseif c == "f" and s:sub(i, i + 4) == "false" then
    return false, i + 5
  elseif c == "n" and s:sub(i, i + 3) == "null" then
    return nil, i + 4
  else
    local num = s:match("^%-?%d+%.?%d*[eE]?[%+%-]?%d*", i)
    if not num then
      error("invalid number at " .. i)
    end
    return tonumber(num), i + #num
  end
end

function JSON.decode(s)
  local v, i = parse_value(s, 1)
  i = skip_ws(s, i)
  if i <= #s then
    error("trailing junk at " .. i)
  end
  return v
end

local encode_value

local function is_array(t)
  local n = 0
  for k in pairs(t) do
    if type(k) ~= "number" or k < 1 or k ~= math.floor(k) then
      return false
    end
    if k > n then
      n = k
    end
  end
  for i = 1, n do
    if t[i] == nil then
      return false
    end
  end
  return true, n
end

local function encode_string(str)
  return '"' .. str:gsub("\\", "\\\\"):gsub('"', '\\"'):gsub("\n", "\\n"):gsub("\r", "\\r"):gsub("\t", "\\t") .. '"'
end

encode_value = function(v)
  local tv = type(v)
  if v == nil then
    return "null"
  elseif tv == "boolean" then
    return v and "true" or "false"
  elseif tv == "number" then
    if v ~= v or v == math.huge or v == -math.huge then
      return "null"
    end
    return string.format("%.17g", v)
  elseif tv == "string" then
    return encode_string(v)
  elseif tv == "table" then
    local arr, n = is_array(v)
    if arr then
      local parts = {}
      for i = 1, n do
        parts[i] = encode_value(v[i])
      end
      return "[" .. table.concat(parts, ",") .. "]"
    else
      local keys = {}
      for k in pairs(v) do
        if type(k) == "string" then
          keys[#keys + 1] = k
        end
      end
      table.sort(keys)
      local parts = {}
      for _, k in ipairs(keys) do
        parts[#parts + 1] = encode_string(k) .. ":" .. encode_value(v[k])
      end
      return "{" .. table.concat(parts, ",") .. "}"
    end
  end
  error("cannot encode " .. tv)
end

function JSON.encode(v)
  return encode_value(v)
end

return JSON
