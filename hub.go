package main

import (
	"encoding/json"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	broadcast chan MessagePayload

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	redisService RedisService

	laravelPresenceChannel string

	laravelChannel string
}

type MessagePayload struct {
	message []byte
	client  *Client
}

func NewHub(redisService RedisService, laravelChannel string, laravelPresenceChannel string) *Hub {
	return &Hub{
		broadcast:              make(chan MessagePayload),
		register:               make(chan *Client),
		unregister:             make(chan *Client),
		clients:                make(map[*Client]bool),
		redisService:           redisService,
		laravelChannel:         laravelChannel,
		laravelPresenceChannel: laravelPresenceChannel,
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			h.redisService.publish(h.laravelChannel, string(message.message))
			possibleMessage := ProcessMessage(message, &h.redisService, h.laravelPresenceChannel)
			if possibleMessage != nil {
				toSendMessage, _ := json.Marshal(possibleMessage)
				for client := range h.clients {
					if client.videoSessionId == possibleMessage.Data.VideoSessionId && client.allowMessage {
						select {
						case client.send <- toSendMessage:
						default:
							close(client.send)
							delete(h.clients, client)
						}
					}
				}
			}
		}
	}
}
