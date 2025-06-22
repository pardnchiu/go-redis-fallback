package redisFallback

import (
	"context"
	"encoding/json"
	"os"
)

func (rf *RedisFallback) Get(key string) (interface{}, error) {
	rf.mutex.RLock()
	isHealth := rf.isHealth
	rf.mutex.RUnlock()

	if isHealth {
		return rf.getFromRedis(key)
	}
	return rf.getFromMemory(key)
}

func (rf *RedisFallback) getFromRedis(key string) (interface{}, error) {
	ctx := context.Background()

	// * Result does not exist or error
	// * Check if the item exists in cache
	if cached, ok := rf.cache.Load(key); ok {
		item := cached.(Cache)

		// * Item is expired
		if isExpired(item) {
			rf.cache.Delete(key)
			rf.removeJSONFile(key)

			return nil, rf.logger.Error(nil, "Not found")
		}

		go rf.syncToRedis(key, item)

		return item.Data, nil
	}

	for i := 0; i < rf.config.Option.MaxRetry; i++ {
		result, err := rf.redis.Get(ctx, key).Result()
		// * Result exists and no error
		if err == nil {
			var item Cache
			// * Parse the JSON data
			if json.Unmarshal([]byte(result), &item) == nil {
				// * Add to memory cache
				rf.cache.Store(key, item)
				return item.Data, nil
			}
		}
	}

	rf.logger.Info("[getFromRedis] Switching to fallback mode")
	rf.mutex.Lock()
	rf.changeToFallbackMode()
	rf.mutex.Unlock()

	return rf.getFromMemory(key)
}

func (rf *RedisFallback) getFromMemory(key string) (interface{}, error) {
	if result, ok := rf.cache.Load(key); ok {
		item := result.(Cache)

		// * Item is expired
		if isExpired(item) {
			rf.cache.Delete(key)

			return nil, rf.logger.Error(nil, "Not found")
		}

		// * Check if the item is valid
		return item.Data, nil
	}

	return rf.loadFromFile(key)
}

func (rf *RedisFallback) loadFromFile(key string) (interface{}, error) {
	path := getPath(rf.config, key)

	// * Check if the file exists
	data, err := os.ReadFile(path.filepath)
	if err != nil {
		return nil, rf.logger.Error(nil, "Not found")
	}

	var item Cache
	// * Parse the JSON data
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, rf.logger.Error(nil, "Failed to parse")
	}

	// * Check if the item is expired
	if isExpired(item) {
		rf.removeJSONFile(key)

		return nil, rf.logger.Error(nil, "Not found")
	}

	// * Update memory cache
	rf.cache.Store(key, item)

	return item.Data, nil
}
