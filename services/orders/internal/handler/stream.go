package handler

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// StreamEvents godoc
// GET /v1/orders/events
//
// Long-lived SSE stream for the vendor dashboard.
// Sends order_created, order_updated, wallet_updated events in real time.
//
// Auth: access token passed as ?token= query param (EventSource can't send headers).
// Replay: send Last-Event-ID header to receive any events missed since disconnect.
func (h *Handler) StreamEvents(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	lastEventID := c.GetHeader("Last-Event-ID")

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // disable nginx/Railway proxy buffering

	ctx := c.Request.Context()
	events, unsub := h.broker.Subscribe(ctx, storeID.String(), lastEventID)
	defer unsub()

	// Initial confirmation with current server timestamp as the event ID.
	// Client stores this as Last-Event-ID for future reconnects.
	connID := fmt.Sprintf("%d", time.Now().UnixMilli())
	fmt.Fprintf(c.Writer, "id: %s\nevent: connected\ndata: {}\n\n", connID)
	c.Writer.Flush()

	// Heartbeat: keeps connection alive through proxies that close idle
	// connections after 30–60s. Does not advance Last-Event-ID.
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			// Emit id: so the browser auto-tracks Last-Event-ID for reconnects.
			fmt.Fprintf(c.Writer, "id: %s\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, ev.Data)
			c.Writer.Flush()

		case <-ticker.C:
			// Heartbeat — no id: so it doesn't shift the client's Last-Event-ID.
			fmt.Fprintf(c.Writer, ": heartbeat\n\n")
			c.Writer.Flush()

		case <-ctx.Done():
			return
		}
	}
}
