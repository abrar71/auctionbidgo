package ws

import (
	"sync"

	"github.com/gorilla/websocket"
)

type room struct {
	mu    sync.RWMutex
	conns map[*clientConn]struct{}
}

func newRoom() *room { return &room{conns: map[*clientConn]struct{}{}} }

func (r *room) add(c *clientConn) {
	r.mu.Lock()
	r.conns[c] = struct{}{}
	r.mu.Unlock()
}

func (r *room) remove(c *clientConn) {
	r.mu.Lock()
	delete(r.conns, c)
	r.mu.Unlock()
	c.rawConn.Close()
}

func (r *room) broadcast(msg []byte) {
	// Take a quick snapshot of the current connections
	r.mu.RLock()
	conns := make([]*clientConn, 0, len(r.conns))
	for c := range r.conns {
		conns = append(conns, c)
	}
	r.mu.RUnlock()

	// Do the I/O outside the lock
	var failed []*clientConn
	for _, c := range conns {
		if err := c.write(websocket.TextMessage, msg); err != nil {
			failed = append(failed, c)
		}
	}
	for _, c := range failed {
		r.remove(c)
	}
}
