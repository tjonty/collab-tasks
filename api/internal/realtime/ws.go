package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
)

// ServeHTTP is the WebSocket endpoint, GET /ws?userId={userId}. Browsers
// can't set headers on a WebSocket, so the user comes in the query.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.URL.Query().Get("userId"))
	known := false
	if err == nil {
		known, err = h.store.UserExists(r.Context(), userID)
		if err != nil {
			slog.Error("ws: check user", "err", err)
			writeError(w, http.StatusServiceUnavailable, apigen.ErrorCodeUnavailable,
				"the database is unreachable; reconnect shortly")
			return
		}
	}
	if !known {
		writeError(w, http.StatusUnauthorized, apigen.ErrorCodeUnknownUser,
			"userId must be the id of a user created with POST /users")
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.opts.OriginPatterns})
	if err != nil {
		return // Accept has replied, e.g. 403 for a foreign origin
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := h.connect(func(code websocket.StatusCode, reason string) {
		conn.Close(code, reason)
		cancel()
	})
	defer h.disconnect(c)

	go h.keepAlive(ctx, c, conn)
	go func() {
		err := h.pump(ctx, c, wsWriter{conn})
		switch {
		case ctx.Err() != nil:
		case errors.Is(err, errStalled):
			// The timed-out write has already cut the connection, so this
			// close frame is best effort; the client sees a drop either way.
			h.dropClient(c, StatusSlowConsumer, "write stalled")
		default:
			slog.Warn("ws: replay failed", "err", err)
			h.dropClient(c, websocket.StatusInternalError, "replay failed; reconnect")
		}
	}()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return // closed by the client, by the hub, or on shutdown
		}
		if typ != websocket.MessageText {
			h.sendError(c, apigen.ErrorCodeValidationFailed, "messages must be JSON text")
			continue
		}
		h.receive(ctx, c, data)
	}
}

// keepAlive pings the client every PingInterval and drops it when a pong
// doesn't come back within PongTimeout.
func (h *Hub) keepAlive(ctx context.Context, c *client, conn *websocket.Conn) {
	t := time.NewTicker(h.opts.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, h.opts.PongTimeout)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					h.dropClient(c, websocket.StatusPolicyViolation, "no pong")
				}
				return
			}
		}
	}
}

// wsWriter writes each message as one text frame.
type wsWriter struct{ conn *websocket.Conn }

func (w wsWriter) Write(ctx context.Context, msg []byte) error {
	return w.conn.Write(ctx, websocket.MessageText, msg)
}

// writeError answers a request that never became a WebSocket.
func writeError(w http.ResponseWriter, status int, code apigen.ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(apigen.Error{Code: code, Message: message}); err != nil {
		slog.Error("ws: write error response", "err", err)
	}
}
