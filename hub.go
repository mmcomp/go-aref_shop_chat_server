package main

import (
	"encoding/json"
	"log"
	"strconv"
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

func (h *Hub) updateUserTokenData(message MessagePayload) {
	tmpMessage := MessageStruct[ChatMessage]{
		Error: "",
	}
	json.Unmarshal(message.message, &tmpMessage)
	token := tmpMessage.Token
	message.client.token = token
	users, err := h.redisService.GetUsers()
	if err == nil {
		idStr, ok := users[token]

		id, convStr := strconv.ParseInt(idStr, 10, 64)
		if ok && convStr == nil {
			message.client.userId = id
		}
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
			h.updateUserTokenData(message)
			log.Println("New Message From Socket userId:", message.client.userId, " token:", message.client.token, " ip:", message.client.ip)
			for client := range h.clients {
				log.Println("Checking socket userId:", client.userId, " token:", client.token, " ip:", client.ip)
				if client != message.client && client.token != "" && client.userId > 0 && client.token != message.client.token && client.userId == message.client.userId && client.ip != message.client.ip {
					shouldDCError := MessageStruct[string]{
						Type:  ErrorTokenInvalid,
						Token: client.token,
						Data:  "socket_login_error",
						Error: "socket_login_error",
					}
					shouldDCErrorStrByte, _ := json.Marshal(shouldDCError)
					client.send <- shouldDCErrorStrByte
				}
			}

			tmpMessage := MessageStruct[ChatMessage]{
				Error: "",
			}
			json.Unmarshal(message.message, &tmpMessage)
			if tmpMessage.Type == Message {
				h.redisService.publish(h.laravelChannel, string(message.message))
			}
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
