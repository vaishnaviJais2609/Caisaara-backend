package websocket

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/here-arjun-1/Caisaara-backend/internal/middleware"
)

type Handler struct {
	Hub *Hub
}

func NewHandler(hub *Hub) *Handler {
	return &Handler{
		Hub: hub,
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (h *Handler) Connect(c *gin.Context) {
	gameID := c.Param("gameID")
	accessToken := c.Query("token")

	if accessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "token required",
		})
		return
	}

	claims, err := middleware.ValidateToken(accessToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "invalid or expired token",
		})
		return
	}

	userID := claims.UserID

	conn, err := upgrader.Upgrade(
		c.Writer,
		c.Request,
		nil,
	)
	if err != nil {
		return
	}

	client := &Client{
		Conn:   conn,
		UserID: userID,
		GameID: gameID,
		Send:   make(chan []byte, 16),
	}

	room := h.Hub.GetOrCreateRoom(gameID)

	room.AddClient(client)

	if room.Count() == 2 {
		sendGameStart(room)
	}

	go h.writePump(client)
	go h.readPump(client, room)
}

func (h *Handler) writePump(client *Client) {
	defer client.Close()

	for message := range client.Send {
		err := client.Conn.WriteMessage(
			websocket.TextMessage,
			message,
		)

		if err != nil {
			return
		}
	}
}

func (h *Handler) readPump(
	client *Client,
	room *Room,
) {
	defer func() {
		room.RemoveClient(client.UserID)
		client.Close()

		if room.Count() == 0 {
			h.Hub.RemoveRoom(room.GameID)
		}
	}()

	for {
		_, message, err := client.Conn.ReadMessage()

		if err != nil {
			return
		}

		room.Broadcast(message)
	}
}

func sendGameStart(room *Room) {
	message := map[string]interface{}{
		"type":    "game_start",
		"game_id": room.GameID,
	}

	data, err := json.Marshal(message)

	if err != nil {
		return
	}

	room.Broadcast(data)
}
