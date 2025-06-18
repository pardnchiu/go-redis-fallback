package redisFallback

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

func (rf *RedisFallback) Set(key string, value interface{}, ttl time.Duration) error {
	rf.mutex.RLock()
	isHealth := rf.isHealth
	rf.mutex.RUnlock()

	item := Cache{
		Key:       key,
		Data:      value,
		Type:      reflect.TypeOf(value).String(),
		Timestamp: time.Now().Unix(),
	}

	if ttl > 0 {
		item.TTL = int64(ttl.Seconds())
	}

	if isHealth {
		return rf.setToRedis(key, item)
	}
	return rf.setToMemory(key, item)
}

func (rf *RedisFallback) setToRedis(key string, cache Cache) error {
	ctx := context.Background()

	data, err := json.Marshal(cache.Data)
	data = []byte(strings.Trim(string(data), "\""))
	if err != nil {
		return rf.logger.error(err, "Failed to parse")
	}

	for i := 0; i < rf.config.Options.MaxRetry; i++ {
		err = rf.redis.Set(ctx, key, data, time.Duration(cache.TTL)*time.Second).Err()
		if err == nil {
			rf.cache.Store(key, cache)
			return nil
		}
	}

	rf.logger.info("[setToRedis] Switching to fallback mode")
	rf.mutex.Lock()
	rf.changeToFallbackMode()
	rf.mutex.Unlock()

	return rf.setToMemory(key, cache)
}

func (rf *RedisFallback) setToMemory(key string, item Cache) error {
	rf.cache.Store(key, item)

	select {
	case rf.writer.queue <- WriteRequest{Key: key, Data: item}:
	default:
		return rf.writer.writeToFile(key, item)
	}

	return nil
}
