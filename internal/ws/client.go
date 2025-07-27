package ws

import (
	"context"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type clientConn struct {
	rawConn *websocket.Conn
	mu      sync.Mutex
}

func (c *clientConn) write(mt websocket.MessageType, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), writeWait)
	defer cancel()

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rawConn.Write(ctx, mt, data)
}

func (c *clientConn) writeJSON(v any) error {
	ctx, cancel := context.WithTimeout(context.Background(), writeWait)
	defer cancel()

	c.mu.Lock()
	defer c.mu.Unlock()
	return wsjson.Write(ctx, c.rawConn, v)
}
