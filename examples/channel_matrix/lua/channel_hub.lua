--[[
  ChannelHub: one 源表, dual need flags (sync_clients + save_db).
  drain() splits mutates into client_batch / db_batch by include_by.
]]

local Record = require("property_record")
local Meta = require("Player_meta")

local Hub = {}
Hub.__index = Hub

local function include_by(need_mask, data_mask)
  return (need_mask & data_mask) == need_mask
end

function Hub.open(opts)
  opts = opts or {}
  local data = opts.obj or Meta.new_default()
  local flags = opts.flags or Meta.FLAGS
  local rec = Record.bind(Meta, {
    obj = data,
    flags = flags,
    need_flag_names = { "sync_clients", "save_db" },
  })
  local self = setmetatable({
    data = data,
    rec = rec,
    flags = flags,
    sync_mask = Record.resolve_mask({ "sync_clients" }, flags),
    db_mask = Record.resolve_mask({ "save_db" }, flags),
  }, Hub)
  return self
end

function Hub:facade()
  return self.rec
end

function Hub:drain_split()
  local batch = self.rec:drain()
  local client, db = {}, {}
  for _, m in ipairs(batch) do
    local flag = m.flag or 0
    if include_by(self.sync_mask, flag) then
      client[#client + 1] = m
    end
    if include_by(self.db_mask, flag) then
      db[#db + 1] = m
    end
  end
  return client, db
end

function Hub:client_view(opts)
  opts = opts or {}
  opts.need_flag_names = opts.need_flag_names or { "sync_clients" }
  return Meta.encode_sync_view(self.data, opts)
end

function Hub:db_view(opts)
  opts = opts or {}
  opts.need_flag_names = opts.need_flag_names or { "save_db" }
  return Meta.encode_sync_view(self.data, opts)
end

return Hub
