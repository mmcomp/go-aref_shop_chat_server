package main

import (
	"encoding/json"
	"log"
	"sort"
	"strconv"
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

func CheckUserToken(redisService *RedisService, token string) (int64, bool, error) {
	users, err := redisService.GetUsers()
	if err == nil {
		idStr, ok := users[token]

		id, _ := strconv.ParseInt(idStr, 10, 64)
		return id, ok, nil
	}

	return 0, false, err
}

func IsUserBlocked(redisService *RedisService, userId int64) (bool, error) {
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

func ProcessMessage(messagePayload MessagePayload, redisService *RedisService, laravelPresenceChannel string) *MessageStruct[ChatMessageWithUser] {
	message := messagePayload.message
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
			messagePayload.client.send <- msg
			return nil
		}
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
			messagePayload.client.send <- msg
			return nil
		}
		userName, nameErr := redisService.GetUserName(userId)
		if nameErr != nil {
			log.Println("Get name error ", nameErr)
			return nil
		}
		switch tmpMessageStruct.Type {
		case Message:
			/*
				{"Type":"MESSAGE","Token":"user_1_token","Data":{"msg":"Salam","video_session_id":1},"Error":""}
			*/
			messageStruct := MessageStruct[ChatMessage]{
				Error: "",
			}
			json.Unmarshal(message, &messageStruct)
			messagePayload.client.videoSessionId = messageStruct.Data.VideoSessionId
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
			addMessageErr := redisService.AddMessage(finalMessage)
			if addMessageErr != nil {
				log.Println("Add message error ", addMessageErr)
				return nil
			}
			return &finalMessage
		case StopMessage:
			messagePayload.client.allowMessage = false
		case StartMessage:
			/*
				{"Type":"START_MESSAGE","Token":"user_1_token","Data":{"video_session_id":1},"Error":""}
			*/
			messageStruct := MessageStruct[StartMessageStruct]{
				Error: "",
			}
			json.Unmarshal(message, &messageStruct)
			messagePayload.client.videoSessionId = messageStruct.Data.VideoSessionId
			messagePayload.client.allowMessage = true
			allMessages, err := redisService.ReadMessages(messagePayload.client.videoSessionId)
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
			messagePayload.client.send <- []byte(resultMessage)
			return nil
		case Presence:
		case FirstPresence:
			/*
				{"Type":"PRESENCE","Token":"user_1_token","Data":{"videoSessionId":1,"type":"online","users_id":1}}
			*/
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
			redisService.rdb.Publish(redisService.ctx, laravelPresenceChannel, string(msg))
		default:
			/*
				{"Type":"HEART_BIT","Token":"token1","Data":"aaa","Error":""}
			*/
			// fmt.Println("Other Message ", tmpMessageStruct)
		}
	}

	return nil
}
