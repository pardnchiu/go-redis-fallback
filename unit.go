package redisFallback

import (
	"crypto/md5"
	"fmt"
	"path/filepath"
	"strconv"
	"time"
)

func getPath(config Config, key string) Path {
	encode := fmt.Sprintf("%x", md5.Sum([]byte(key)))
	layer1 := encode[0:2]
	layer2 := encode[2:4]
	layer3 := encode[4:6]
	filename := encode + ".json"
	folderPath := filepath.Join(config.Option.DBPath, strconv.Itoa(config.Redis.DB), layer1, layer2, layer3)

	return Path{
		folderPath: folderPath,
		filepath:   filepath.Join(folderPath, filename),
		filename:   filename,
	}
}

func isExpired(item Cache) bool {
	if item.TTL <= 0 {
		return false
	}
	return time.Now().Unix() > item.Timestamp+item.TTL
}
