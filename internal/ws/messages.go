package ws

import "encoding/json"

// Envelope wraps every WS frame.
type Envelope struct {
	Event string          `json:"event"`          // e.g. "auctions/bid"
	Body  json.RawMessage `json:"body,omitempty"` // arbitrary JSON object
}

// ──────────────────────────── Request / Response DTOs ─────────────────────────

// BidRequest is the body for "auctions/bid".
type BidRequest struct {
	Amount float64 `json:"amount" validate:"gt=0"`
}

// Empty ACK body (useful for many handlers).
type AckBody struct{}

// ErrorBody is returned for failures.
type ErrorBody struct {
	Error string `json:"error"`
}
