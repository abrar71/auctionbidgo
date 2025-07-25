#!lua name=auction_stop
--[[

  KEYS[1] = "auc:<id>"
  KEYS[2] = "auc_t:<id>"

]]
local function auction_stop(keys, argv)
  local hashKey   = keys[1]
  local timerKey  = keys[2]
  local auctionID = string.sub(hashKey, 5)

  local snapshot  = redis.call('HGETALL', hashKey)
  if next(snapshot) ~= nil then
    redis.call('PUBLISH', 'auc:' .. auctionID .. ':events', cjson.encode({
      version = 1,
      event   = 'stop',
      data    = snapshot
    }))
  end

  redis.call('DEL', hashKey, timerKey)
  redis.call('SREM', 'aucs:active', hashKey)
  redis.call('SADD', 'aucs:ended', hashKey)
  return 1
end
redis.register_function('auction_stop', auction_stop)
