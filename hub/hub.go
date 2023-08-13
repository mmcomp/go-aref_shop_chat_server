package hub

import (
	redisPkg "github.com/mmcomp/go-aref_shop_chat_server/redis"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered Clients.
	Clients map[*Client]bool

	// Inbound messages from the clients.
	broadcast chan MessagePayload

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	RedisService redisPkg.RedisService

	LaravelPresenceChannel string

	LaravelChannel string
}

func NewHub(redisService redisPkg.RedisService, laravelChannel string, laravelPresenceChannel string) *Hub {
	return &Hub{
		broadcast:              make(chan MessagePayload),
		register:               make(chan *Client),
		unregister:             make(chan *Client),
		Clients:                make(map[*Client]bool),
		RedisService:           redisService,
		LaravelChannel:         laravelChannel,
		LaravelPresenceChannel: laravelPresenceChannel,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.Clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.Clients[client]; ok {
				delete(h.Clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			StartMessageProcess(h, message)
		}
	}
}
