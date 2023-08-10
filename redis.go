package main

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisService struct {
	rdb          *redis.Client
	ctx          context.Context
	userHash     string
	messageHash  string
	messageCount int
}

func (r *RedisService) Set(key string, value interface{}, expiration time.Duration) error {
	return r.rdb.Set(r.ctx, key, value, expiration).Err()
}

func (r *RedisService) Get(key string) (string, error) {
	val, err := r.rdb.Get(r.ctx, key).Result()

	if err == redis.Nil {
		return "", errors.New("'" + key + "' not found")
	}

	return val, err
}

func (r *RedisService) publish(channel string, payload string) error {
	return r.rdb.Publish(r.ctx, channel, payload).Err()
}

// func (r *RedisService) SubscribeChannel(channel string) {
// 	pubSub := r.rdb.Subscribe(r.ctx, channel)
// 	defer pubSub.Close()

// 	// ch := pubSub.Channel()
// 	// for msg := range ch {
// 	// 	// fmt.Println(msg.Channel, msg.Payload)
// 	// }
// }

func (r *RedisService) GetUsers() (map[string]string, error) {
	keys, err := r.rdb.Keys(r.ctx, r.userHash+"user_*").Result()
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, userKey := range keys {
		token, e := r.Get(userKey)
		if e == nil {
			result[token] = strings.ReplaceAll(userKey, r.userHash+"user_", "")
		}
	}

	return result, nil
}

func (r *RedisService) GetBlockedUserIds() ([]int64, error) {
	value, err := r.Get(r.userHash + "blocked_users")
	if err != nil {
		return []int64{}, err
	}
	var ids []int64
	json.Unmarshal([]byte(value), &ids)
	return ids, nil
}

func (r *RedisService) GetUserName(id int64) (string, error) {
	return r.Get(r.userHash + "name_" + strconv.FormatInt(id, 10))
}

func (r *RedisService) TrimMessages(mainKey string) error {
	allMessages, err := r.rdb.HGetAll(r.ctx, mainKey).Result()
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(allMessages))

	for k := range allMessages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	extra := len(allMessages) - r.messageCount
	if extra > 0 {
		var i int = 0
		for _, key := range keys {
			if i < extra {
				err := r.rdb.HDel(r.ctx, mainKey, key).Err()
				if err != nil {
					return err
				}
			}
			i++
		}
	}

	return nil
}

func (r *RedisService) ReadMessages(videoSessionId int64) (map[string]string, error) {
	mainKey := r.messageHash + "_" + strconv.FormatInt(videoSessionId, 10)
	return r.rdb.HGetAll(r.ctx, mainKey).Result()
}

func (r *RedisService) AddMessage(message MessageStruct[ChatMessageWithUser]) error {
	mainKey := r.messageHash + "_" + strconv.FormatInt(message.Data.VideoSessionId, 10)
	key := time.Now().UnixMilli()
	value, _ := json.Marshal(message)
	err := r.rdb.HSet(r.ctx, mainKey, key, value).Err()
	if err != nil {
		return err
	}

	return r.TrimMessages(mainKey)
}

func NewRedisService(ctx context.Context, addr string, channel string, userHash string, messageHash string, messageCount int, password string, db int) RedisService {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	result := RedisService{
		rdb:          rdb,
		ctx:          ctx,
		userHash:     userHash,
		messageHash:  messageHash,
		messageCount: messageCount,
	}

	// go result.SubscribeChannel(channel)

	return result
}
