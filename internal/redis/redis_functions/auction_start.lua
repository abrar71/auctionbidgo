#!lua name=auction_start
--[[

  KEYS[1] = "auc:<id>"
  KEYS[2] = "auc_t:<id>"

  ARGV[1] = sellerId
  ARGV[2] = startsAtUnix
  ARGV[3] = endsAtUnix
  ARGV[4] = ttlSeconds

]]
local function auction_start(keys, argv)
  local hashKey   = keys[1]
  local timerKey  = keys[2]
  local auctionID = string.sub(hashKey, 5)

  if redis.call('EXISTS', hashKey) == 1 then
    return { err = 'already_started' }
  end

  local state = redis.call('HGET', hashKey, 'st')
  if state == 'RUNNING' or state == 'FINISHED' then
    return redis.error_reply('already_started')
  end

  redis.call('HSET', hashKey,
    'sid', argv[1],
    'sa', argv[2],
    'ea', argv[3],
    'st', 'RUNNING',
    'hb', 0,
    'hbid', ''
  )

  redis.call('SET', timerKey, '1', 'EX', argv[4])
  redis.call('SADD', 'aucs:active', hashKey)

  redis.call('PUBLISH', 'auc:' .. auctionID .. ':events', cjson.encode({
    version = 1,
    event   = 'start',
    endsAt  = argv[3]
  }))
  return 1
end
redis.register_function('auction_start', auction_start)
