package websocket

import "sync"

type Hub struct {
	Rooms map[string]*Room

	Mutex sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		Rooms: make(map[string]*Room),
	}
}

func (h *Hub) GetOrCreateRoom(gameID string) *Room {

	h.Mutex.Lock()
	defer h.Mutex.Unlock()

	room, exists := h.Rooms[gameID]

	if !exists {
		room = NewRoom(gameID)
		h.Rooms[gameID] = room
	}

	return room
}

func (h *Hub) RemoveRoom(gameID string) {

	h.Mutex.Lock()
	defer h.Mutex.Unlock()

	delete(h.Rooms, gameID)
}
