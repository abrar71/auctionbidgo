package ws

import (
	"sync"

	"github.com/coder/websocket"
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
	_ = c.rawConn.Close(websocket.StatusNormalClosure, "")
}

func (r *room) broadcast(msg []byte) {
	r.mu.RLock()
	conns := make([]*clientConn, 0, len(r.conns))
	for c := range r.conns {
		conns = append(conns, c)
	}
	r.mu.RUnlock()

	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(cc *clientConn) {
			defer wg.Done()
			_ = cc.write(websocket.MessageText, msg)
		}(c)
	}
	wg.Wait() // ensures ordering; remove if "at‑least‑once" is sufficient
}
