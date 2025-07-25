package ws

import (
	"sync"
)

// Hub keeps client sets per auctionID.
type Hub struct {
	rooms sync.Map // auctionID -> *room
}

func NewHub() *Hub { return &Hub{} }

// Broadcast is called by the Redis subscriber.
func (h *Hub) Broadcast(auctionID string, msg []byte) {
	if v, ok := h.rooms.Load(auctionID); ok {
		v.(*room).broadcast(msg)
	}
}
func (h *Hub) Join(auctionID string, c *clientConn) {
	r, _ := h.rooms.LoadOrStore(auctionID, newRoom())
	r.(*room).add(c)
}

func (h *Hub) Leave(auctionID string, c *clientConn) {
	if v, ok := h.rooms.Load(auctionID); ok {
		v.(*room).remove(c)
	}
}
