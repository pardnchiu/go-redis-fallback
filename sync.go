package redisFallback

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (rf *RedisFallback) syncToRedis(key string, cache Cache) {
	ctx := context.Background()
	data, err := json.Marshal(cache.Data)
	data = []byte(strings.Trim(string(data), "\""))
	if err != nil {
		rf.logger.error(err, "Failed to parse")
		return
	}
	rf.redis.Set(ctx, key, data, time.Duration(cache.TTL)*time.Second)
}

func (rf *RedisFallback) changeToFallbackMode() {
	// rf.sendEmail("Redis connection failed, starting health check")
	rf.isHealth = false

	if rf.checker != nil {
		return
	}

	rf.checker = time.NewTicker(rf.config.Options.TimeToCheck)
	go func() {
		for range rf.checker.C {
			ctx := context.Background()
			if err := rf.redis.Ping(ctx).Err(); err == nil {
				rf.mutex.Lock()
				go rf.changeToNormalMode()
				rf.mutex.Unlock()

				rf.checker.Stop()
				rf.checker = nil
				return
			}
		}
	}()
}

func (rf *RedisFallback) changeToNormalMode() error {
	folderPath := filepath.Join(rf.config.Options.DBPath, strconv.Itoa(rf.config.Redis.DB))

	var files []string
	err := filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".json") {
			files = append(files, path)
		}
		return nil
	})

	if err != nil {
		return rf.logger.error(err, "Failed to search folder")
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			rf.logger.error(err, "Failed to read file")
			continue
		}

		var cache Cache
		if err := json.Unmarshal(data, &cache); err != nil {
			rf.logger.error(err, "Failed to parse")
			continue
		}

		rf.cache.Store(cache.Key, cache)
	}

	rf.syncMemoryToRedis()
	if err := rf.cleanupLocalFile(); err != nil {
		rf.logger.error(err, "Failed to cleanup")
	}

	rf.isHealth = true

	return nil
}

func (rf *RedisFallback) syncMemoryToRedis() {
	if !rf.isRecovering.CompareAndSwap(false, true) {
		rf.logger.info("Already running recovery")
		return
	}

	defer rf.isRecovering.Store(false)

	ctx := context.Background()
	pipe := rf.redis.Pipeline()
	count := 0
	now := time.Now().Unix()

	rf.cache.Range(func(key, value interface{}) bool {
		item := value.(Cache)
		if !isExpired(item) {
			data, err := json.Marshal(item.Data)
			data = []byte(strings.Trim(string(data), "\""))
			if err != nil {
				rf.logger.error(err, "Failed to parse")
			} else {
				remainingTTL := time.Duration(item.Timestamp+item.TTL-now) * time.Second
				if remainingTTL > 0 {
					pipe.Set(ctx, key.(string), data, remainingTTL)
				}
			}

			count++
			if count%100 == 0 {
				pipe.Exec(ctx)
				pipe = rf.redis.Pipeline()
			}
		}
		return true
	})

	if count%100 != 0 {
		pipe.Exec(ctx)
	}
}

func (rf *RedisFallback) startMemoryCleanup() {
	if rf.isRecovering.Load() {
		rf.logger.info("Recovery is in progress")
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			rf.cache.Range(func(key, value interface{}) bool {
				item := value.(Cache)
				if isExpired(item) {
					rf.cache.Delete(key)
					rf.removeJSONFile(key.(string))
				}
				return true
			})
		}
	}()
}

func (rf *RedisFallback) cleanupLocalFile() error {
	var folderPath = rf.config.Options.DBPath + "/" + fmt.Sprintf("%d", rf.config.Redis.DB)

	filesRemoved := 0
	err := filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			rf.logger.error(err, "Failed to search folder")
			return nil
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".json") {
			if err := os.Remove(path); err != nil {
				rf.logger.error(err, "Failed to remove file")
			} else {
				filesRemoved++
			}
		}
		return nil
	})

	if err != nil {
		rf.logger.error(err, "Failed to remove file")
		return err
	}

	rf.removeEmptyFolder(folderPath)

	return nil
}

func (rf *RedisFallback) removeEmptyFolder(root string) int {
	dirsRemoved := 0

	var walkFn func(path string) error
	walkFn = func(path string) error {
		entries, err := os.ReadDir(path)
		if err != nil {
			rf.logger.error(err, "Failed to read path")
			return nil
		}

		for _, entry := range entries {
			if entry.IsDir() {
				subpath := filepath.Join(path, entry.Name())
				walkFn(subpath)
			}
		}

		if len(entries) == 0 && path != root {
			err := os.Remove(path)
			if err != nil {
				rf.logger.error(err, "Failed to remove path")
			} else {
				dirsRemoved++
			}
		}
		return nil
	}

	walkFn(root)
	return dirsRemoved
}
