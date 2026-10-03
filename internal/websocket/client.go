package websocket

import (
	"sync"

	"github.com/gorilla/websocket"
)

type Client struct {
	Conn   *websocket.Conn
	UserID int64
	GameID string

	Send chan []byte

	once sync.Once
}

func (c *Client) Close() {
	c.once.Do(func() {
		close(c.Send)
		_ = c.Conn.Close()
	})
}
