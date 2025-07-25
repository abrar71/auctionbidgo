package auctionhandler

import "time"

type StartAuctionBody struct {
	SellerID string    `json:"seller_id" binding:"required" example:"seller123"`
	EndsAt   time.Time `json:"ends_at"   binding:"required" example:"2025-07-27T16:05:05Z"`
} // @name StartAuctionRequest

type PlaceBidBody struct {
	BidderID string  `json:"bidder_id" binding:"required"      example:"user123"`
	Amount   float64 `json:"amount"    binding:"required,gt=0" example:"5"`
} // @name PlaceBidRequest

type ErrorResponse struct {
	Error string `json:"error"`
} // @name ErrorResponse

type ListAuctionsQuery struct {
	Status string `form:"status"  binding:"omitempty,oneof=RUNNING FINISHED"`
	Limit  int    `form:"limit,default=10"  binding:"gte=0,lte=100"`
	Offset int    `form:"offset,default=0"  binding:"gte=0"`
} // @name ListAuctionsQuery
