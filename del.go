package redisFallback

import (
	"context"
)

func (rf *RedisFallback) Del(key string) error {
	rf.mutex.Lock()
	isHealth := rf.isHealth
	rf.mutex.Unlock()

	rf.cache.Delete(key)
	rf.removeJSONFile(key)

	if isHealth {
		ctx := context.Background()
		err := rf.redis.Del(ctx, key).Err()
		if err != nil {
			return rf.logger.error(err, "Failed to delete")
		}
	}
	return nil
}
