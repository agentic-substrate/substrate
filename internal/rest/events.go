// Package rest is the /v1 adapter and CLI surface (EDD §4.2).
package rest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const eventBuffer = 8

type hub struct {
	mu      sync.Mutex
	buf     int
	subs    map[chan []byte]struct{}
	cancel  context.CancelFunc
	started bool
	ready   chan struct{}
}

func newHub(buf int) *hub {
	return &hub{
		buf:   buf,
		subs:  make(map[chan []byte]struct{}),
		ready: make(chan struct{}),
	}
}

func (h *hub) subscribe() chan []byte {
	ch := make(chan []byte, h.buf)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, ch)
}

func (h *hub) broadcast(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- msg:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}

func (h *hub) stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
}

func (h *hub) waitReady(d time.Duration) error {
	select {
	case <-h.ready:
		return nil
	case <-time.After(d):
		return fmt.Errorf("LISTEN substrate_audit not ready after %s", d)
	}
}

func (h *hub) ensureListen(pool *pgxpool.Pool) {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return
	}
	h.started = true
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	ready := h.ready
	h.mu.Unlock()
	go h.listen(ctx, pool, ready)
}

func (h *hub) listen(ctx context.Context, pool *pgxpool.Pool, ready chan struct{}) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `LISTEN substrate_audit`); err != nil {
		return
	}
	select {
	case <-ready:
	default:
		close(ready)
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return
		}
		if n != nil {
			h.broadcast([]byte(n.Payload))
		}
	}
}
