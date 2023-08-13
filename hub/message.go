package hub

import (
	"encoding/json"
	"log"
	"sort"
	"strconv"

	redisPkg "github.com/mmcomp/go-aref_shop_chat_server/redis"
)

type MessageType string

const (
	Message             MessageType = "MESSAGE"
	StartMessage        MessageType = "START_MESSAGE"
	StopMessage         MessageType = "STOP_MESSAGE"
	HeartBit            MessageType = "HEART_BIT"
	ErrorMessageInvalid MessageType = "ERROR_MESSAGE_INVALID"
	ErrorTokenInvalid   MessageType = "ERROR_TOKEN_INVALID"
	Presence            MessageType = "PRESENCE"
	FirstPresence       MessageType = "FIRST_PRESENCE"
)

type StartMessageStruct struct {
	VideoSessionId int64 `json:"video_session_id"`
}

type ChatMessage struct {
	Msg            string `json:"msg"`
	VideoSessionId int64  `json:"video_session_id"`
}

type ChatMessageWithUser struct {
	Msg            string `json:"msg"`
	VideoSessionId int64  `json:"video_session_id"`
	Id             int64  `json:"id"`
	Name           string `json:"name"`
}

type Absence struct {
	Type           string `json:"type"`
	UserId         int64  `json:"users_id"`
	VideoSessionId int64  `json:"videoSessionId"`
	IsFirst        bool   `json:"isFirst"`
}

type MessageData interface {
	ChatMessage | string | int64 | ChatMessageWithUser | StartMessageStruct | Absence
}

type MessageStruct[T MessageData] struct {
	Type  MessageType
	Token string
	Data  T
	Error string
}

func CheckUserToken(redisService *redisPkg.RedisService, token string) (int64, bool, error) {
	users, err := redisService.GetUsers()
	if err == nil {
		idStr, ok := users[token]

		id, _ := strconv.ParseInt(idStr, 10, 64)
		return id, ok, nil
	}

	return 0, false, err
}

func IsUserBlocked(redisService *redisPkg.RedisService, userId int64) (bool, error) {
	blockedUsers, err := redisService.GetBlockedUserIds()
	if err != nil {
		return false, err
	}

	for _, id := range blockedUsers {
		if id == userId {
			return true, nil
		}
	}

	return false, nil
}

func UpdateUserTokenData(redisService redisPkg.RedisService, message MessagePayload) {
	tmpMessage := MessageStruct[ChatMessage]{
		Error: "",
	}
	json.Unmarshal(message.Message, &tmpMessage)
	token := tmpMessage.Token
	message.Client.Token = token
	users, err := redisService.GetUsers()
	if err == nil {
		idStr, ok := users[token]

		id, convStr := strconv.ParseInt(idStr, 10, 64)
		if ok && convStr == nil {
			message.Client.UserId = id
		}
	}
}

func CheckReplicatedSockets(h *Hub, message MessagePayload) {
	for client := range h.Clients {
		if client != message.Client && client.Token != "" && client.UserId > 0 && client.Token != message.Client.Token && client.UserId == message.Client.UserId && client.Ip != message.Client.Ip {
			shouldDCError := MessageStruct[string]{
				Type:  ErrorTokenInvalid,
				Token: client.Token,
				Data:  "socket_login_error",
				Error: "socket_login_error",
			}
			shouldDCErrorStrByte, _ := json.Marshal(shouldDCError)
			client.send <- shouldDCErrorStrByte
		}
	}
}

func SendToLaravel(h *Hub, message MessagePayload) {
	tmpMessage := MessageStruct[ChatMessage]{
		Error: "",
	}
	json.Unmarshal(message.Message, &tmpMessage)
	if tmpMessage.Type == Message {
		h.RedisService.Publish(h.LaravelChannel, string(message.Message))
	}
}

func Broadcast(h *Hub, message MessagePayload) {
	possibleMessage := ProcessMessage(message, &h.RedisService, h.LaravelPresenceChannel)
	if possibleMessage != nil {
		toSendMessage, _ := json.Marshal(possibleMessage)
		for client := range h.Clients {
			if client.VideoSessionId == possibleMessage.Data.VideoSessionId && client.AllowMessage {
				select {
				case client.send <- toSendMessage:
				default:
					close(client.send)
					delete(h.Clients, client)
				}
			}
		}
	}
}

func StartMessageProcess(h *Hub, messagePayload MessagePayload) {
	UpdateUserTokenData(h.RedisService, messagePayload)
	CheckReplicatedSockets(h, messagePayload)
	SendToLaravel(h, messagePayload)
	Broadcast(h, messagePayload)
}

func ProcessMessage(messagePayload MessagePayload, redisService *redisPkg.RedisService, laravelPresenceChannel string) *MessageStruct[ChatMessageWithUser] {
	message := messagePayload.Message
	if json.Valid(message) {
		tmpMessageStruct := MessageStruct[string]{
			Data:  "",
			Error: "",
		}
		json.Unmarshal(message, &tmpMessageStruct)

		userId, tokenOk, tokenErr := CheckUserToken(redisService, tmpMessageStruct.Token)
		if tokenErr != nil {
			log.Println("Check token error ", tokenErr)
			return nil
		}
		if !tokenOk {
			unauthorizedMessage := MessageStruct[string]{
				Type:  ErrorTokenInvalid,
				Data:  "",
				Error: "token is invalid!",
				Token: tmpMessageStruct.Token,
			}
			msg, _ := json.Marshal(unauthorizedMessage)
			messagePayload.Client.send <- msg
			return nil
		}
		messagePayload.Client.Token = tmpMessageStruct.Token
		messagePayload.Client.UserId = userId
		isBlocked, blockedErr := IsUserBlocked(redisService, userId)
		if blockedErr != nil {
			log.Println("Check blocked error ", blockedErr)
			return nil
		}
		if isBlocked {
			blockedMessage := MessageStruct[string]{
				Type:  Message,
				Data:  "",
				Error: "User blocked",
				Token: tmpMessageStruct.Token,
			}
			msg, _ := json.Marshal(blockedMessage)
			messagePayload.Client.send <- msg
			return nil
		}
		userName, nameErr := redisService.GetUserName(userId)
		if nameErr != nil {
			log.Println("Get name error ", nameErr)
			return nil
		}
		switch tmpMessageStruct.Type {
		case Message:
			messageStruct := MessageStruct[ChatMessage]{
				Error: "",
			}
			json.Unmarshal(message, &messageStruct)
			messagePayload.Client.VideoSessionId = messageStruct.Data.VideoSessionId
			finalMessage := MessageStruct[ChatMessageWithUser]{
				Type:  tmpMessageStruct.Type,
				Token: tmpMessageStruct.Token,
				Error: "",
				Data: ChatMessageWithUser{
					VideoSessionId: messageStruct.Data.VideoSessionId,
					Msg:            messageStruct.Data.Msg,
					Id:             userId,
					Name:           userName,
				},
			}
			finalMessageByte, _ := json.Marshal(finalMessage)
			addMessageErr := redisService.AddMessage(finalMessageByte, messagePayload.Client.VideoSessionId)
			if addMessageErr != nil {
				log.Println("Add message error ", addMessageErr)
				return nil
			}
			return &finalMessage
		case StopMessage:
			messagePayload.Client.AllowMessage = false
		case StartMessage:
			messageStruct := MessageStruct[StartMessageStruct]{
				Error: "",
			}
			json.Unmarshal(message, &messageStruct)
			messagePayload.Client.VideoSessionId = messageStruct.Data.VideoSessionId
			messagePayload.Client.AllowMessage = true
			allMessages, err := redisService.ReadMessages(messagePayload.Client.VideoSessionId)
			if err != nil {
				log.Println("Read message error ", err)
				return nil
			}

			keys := make([]string, 0, len(allMessages))

			for k := range allMessages {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			allMessagesStruct := []ChatMessageWithUser{}
			for _, k := range keys {
				var aMessage MessageStruct[ChatMessageWithUser]
				json.Unmarshal([]byte(allMessages[k]), &aMessage)
				allMessagesStruct = append(allMessagesStruct, aMessage.Data)
			}
			data, _ := json.Marshal(allMessagesStruct)
			resultMessage := `{"Type":"MESSAGE","Error":"","Token":"` + tmpMessageStruct.Token + `","Data":` + string(data) + `}`
			messagePayload.Client.send <- []byte(resultMessage)
			return nil
		case Presence:
		case FirstPresence:
			messageStruct := MessageStruct[Absence]{
				Error: "",
				Data: Absence{
					IsFirst: false,
				},
			}
			json.Unmarshal(message, &messageStruct)
			if messageStruct.Type == FirstPresence {
				messageStruct.Data.IsFirst = true
			}

			msg, _ := json.Marshal(messageStruct.Data)
			redisService.Publish(laravelPresenceChannel, string(msg))
		}
	}

	return nil
}
