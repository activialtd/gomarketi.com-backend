package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	HandshakeTimeout: 5 * time.Second,
	ReadBufferSize:   256,
	WriteBufferSize:  4096,
	// CORS is enforced by the gateway — the orders service trusts X-User-ID.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsMessage is the JSON envelope sent to WebSocket clients.
type wsMessage struct {
	Type string          `json:"type"`
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

// WsEvents godoc
// GET /v1/orders/ws
//
// WebSocket stream for the vendor dashboard.
// Sends order_created, order_updated, wallet_updated events in real time.
//
// Auth: access token passed as ?token= query param (browser WebSocket cannot send headers).
// Replay: pass ?last_id= to replay events missed since last disconnect.
func (h *Handler) WsEvents(c *gin.Context) {
	storeID, ok := h.callerStoreID(c)
	if !ok {
		return
	}

	lastEventID := c.Query("last_id")

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade already wrote the HTTP error response.
		h.log.Error().Err(err).Msg("ws: upgrade failed")
		return
	}
	defer conn.Close()

	ctx := c.Request.Context()
	events, unsub := h.broker.Subscribe(ctx, storeID.String(), lastEventID)
	defer unsub()

	// Read pump — gorilla requires that reads and writes happen in separate
	// goroutines. This one drives control-frame handling (pong, close) and
	// signals when the client has gone away via readDone.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		conn.SetReadLimit(512)
		_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		conn.SetPongHandler(func(string) error {
			return conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			var raw json.RawMessage
			if err := json.Unmarshal([]byte(ev.Data), &raw); err != nil {
				raw = json.RawMessage(fmt.Sprintf("%q", ev.Data))
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(wsMessage{Type: ev.Type, ID: ev.ID, Data: raw}); err != nil {
				return
			}

		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-readDone:
			return

		case <-ctx.Done():
			return
		}
	}
}
