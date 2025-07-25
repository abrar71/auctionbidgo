package ws

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type clientConn struct {
	rawConn *websocket.Conn
	mu      sync.Mutex
}

func (c *clientConn) write(mt int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_ = c.rawConn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.rawConn.WriteMessage(mt, data) // Text/Binary only
}

func (c *clientConn) writeJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_ = c.rawConn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.rawConn.WriteJSON(v)
}
