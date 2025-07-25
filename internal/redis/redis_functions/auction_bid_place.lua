#!lua name=auction_place_bid
--[[

  KEYS[1] = "auc:<id>"
  KEYS[2] = "auc_t:<id>"
  ARGV[1] = bidderId
  ARGV[2] = amountFloat
  ARGV[3] = placedAtUnix
  ARGV[4] = minIncrement (optional; "0" if none)

]]
local function auction_place_bid(keys, argv)
  local akey      = keys[1]
  local timerKey  = keys[2]
  local bidder    = argv[1]
  local amount    = tonumber(argv[2])
  local ts        = tonumber(argv[3])
  local minInc    = tonumber(argv[4] or "0")
  local auctionID = string.sub(akey, 5)

  -- Reject if auction is closed or timer key already expired
  if redis.call('HGET', akey, 'st') ~= 'RUNNING'
      or redis.call('EXISTS', timerKey) == 0 then
    return redis.error_reply('auction_closed')
  end

  -- Safety precaution: compare the bid at timestamp against stored auctions ends‑at timestamp
  local ea = tonumber(redis.call('HGET', akey, 'ea') or '0')
  if ts >= ea then
    return redis.error_reply('auction_closed')
  end

  local current = tonumber(redis.call('HGET', akey, 'hb') or '0')
  -- same price -> explicit error
  if amount == current then
    return redis.error_reply('bid_equal')
  end

  -- lower than current -> different error
  if amount < current then
    return redis.error_reply('bid_below_current')
  end

  -- lower than (current + minInc) -> different error
  if amount < current + minInc then
    return redis.error_reply('bid_below_increment')
  end

  redis.call('HSET', akey, 'hb', amount, 'hbid', bidder, 'ts', ts)

  -- append to global stream for persistence
  redis.call('XADD', 'bids_stream', '*',
    'aid', auctionID,
    'bidder', bidder,
    'amount', amount,
    'at', ts)

  redis.call('PUBLISH', 'auc:' .. auctionID .. ':events', cjson.encode({
    version = 1,
    event   = 'bid',
    bidder  = bidder,
    amount  = amount,
    at      = ts
  }))
  return 1
end
redis.register_function('auction_place_bid', auction_place_bid)
