package handler

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// StreamEvents godoc
// GET /v1/orders/events
// Streams order and wallet change notifications as Server-Sent Events.
// The client must send the access token as a ?token= query param because
// the browser's EventSource API does not support custom headers.
func (h *Handler) StreamEvents(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // disable nginx/Railway proxy buffering

	events, unsub := h.broker.Subscribe(storeID.String())
	defer unsub()

	// Send initial connection confirmation.
	fmt.Fprintf(c.Writer, "event: connected\ndata: {}\n\n")
	c.Writer.Flush()

	// Heartbeat ticker — keeps the connection alive through proxies that
	// close idle connections after 30–60 s.
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", ev.Type, ev.Data)
			c.Writer.Flush()
		case <-ticker.C:
			fmt.Fprintf(c.Writer, "event: ping\ndata: {}\n\n")
			c.Writer.Flush()
		case <-ctx.Done():
			return
		}
	}
}
