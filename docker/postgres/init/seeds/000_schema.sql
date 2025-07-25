create table if not exists auctions (
  id         text primary key,
  seller_id  text not null,
  item       text not null,
  starts_at  timestamptz not null,
  ends_at    timestamptz not null,
  status     text not null,
  high_bid   numeric,
  high_bidder text
);

create table if not exists bids (
  id         bigserial primary key,
  auction_id text references auctions(id),
  bidder_id  text not null,
  amount     numeric not null,
  placed_at  timestamptz not null default now()
);

CREATE INDEX IF NOT EXISTS bids_auction_id_idx ON bids (auction_id);
