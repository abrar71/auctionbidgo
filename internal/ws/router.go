package ws

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// internal (untyped) handler signature.
type rawHandler func(ctx context.Context, c *ConnContext, body json.RawMessage) (any, error)

// Router keeps a map[event]handler, à‑la gin.Engine.
type Router struct {
	mu       sync.RWMutex
	handlers map[string]rawHandler
}

func NewRouter() *Router { return &Router{handlers: make(map[string]rawHandler)} }

// Register binds an event to a strongly‑typed handler.
func Register[Req any, Res any](
	r *Router,
	event string,
	h func(ctx context.Context, c *ConnContext, req Req) (Res, error),
) {
	if event == "" {
		panic("ws router: empty event")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[event] = func(ctx context.Context, c *ConnContext, body json.RawMessage) (any, error) {
		var req Req
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, err
			}
		}
		return h(ctx, c, req)
	}
}

// dispatch is called by the server’s reader loop.
func (r *Router) dispatch(ctx context.Context, c *ConnContext, env Envelope) (any, error) {
	r.mu.RLock()
	h, ok := r.handlers[env.Event]
	r.mu.RUnlock()
	if !ok {
		return nil, errors.New("unknown_event")
	}
	return h(ctx, c, env.Body)
}
