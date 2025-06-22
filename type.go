package redisFallback

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	goLogger "github.com/pardnchiu/go-logger"
	"github.com/redis/go-redis/v9"
)

const (
	defaultLogPath      = "./logs/redisFallback"
	defaultLogMaxSize   = 16 * 1024 * 1024
	defaultLogMaxBackup = 5
	defaultDBPath       = "./files/redisFallback/db"
	defaultMaxRetry     = 3
	defaultMaxQueue     = 1000            // 最大排隊長度，預設 1000
	defaultTimeToWrite  = 3 * time.Second // 預設 Fallback 模式下寫入時間間隔
	defaultTimeToCheck  = 1 * time.Minute // 預設健康檢查時間間隔
)

// * 繼承至 pardnchiu/go-logger
type Log = goLogger.Log
type Logger = goLogger.Logger

type Config struct {
	Redis  *Redis       `json:"redis"`            // Redis 設定
	Log    *Log         `json:"log,omitempty"`    // 日誌設定
	Option *Options     `json:"option,omitempty"` // 選項設定
	Email  *EmailConfig `json:"email,omitempty"`  // Email 通知設定
}

type Redis struct {
	Host     string `json:"host"`               // Redis 主機位址
	Port     int    `json:"port"`               // Redis 連接埠
	Password string `json:"password,omitempty"` // Redis 密碼
	DB       int    `json:"db"`                 // Redis 資料庫編號
}

type Options struct {
	DBPath      string        // 預設資料庫路徑
	MaxRetry    int           // 最大重試次數，預設 3
	MaxQueue    int           // 最大排隊長度，預設 1000
	TimeToWrite time.Duration // Fallback 模式下寫入時間間隔，預設 3 秒
	TimeToCheck time.Duration // 健康檢查時間間隔，預設 1 分鐘
}

type RedisFallback struct {
	config       Config
	logger       *Logger
	redis        *redis.Client
	context      context.Context
	mutex        sync.RWMutex
	cache        sync.Map
	isHealth     bool
	isRecovering atomic.Bool
	checker      *time.Ticker
	writer       *Writer
}

type Writer struct {
	config  Config
	logger  *Logger
	mutex   sync.Mutex
	queue   chan WriteRequest
	pending map[string]interface{}
	timer   *time.Ticker
}

type WriteRequest struct {
	Key  string
	Data interface{}
}

type Cache struct {
	Key       string      `json:"key"`
	Data      interface{} `json:"data"`
	Type      string      `json:"type"`
	Timestamp int64       `json:"timestamp"`
	TTL       int64       `json:"ttl,omitempty"`
}

type Path struct {
	folderPath string
	filepath   string
	filename   string
}

type EmailConfig struct {
	Host     string                                 `json:"host"`
	Port     int                                    `json:"port"`
	Username string                                 `json:"username"`
	Password string                                 `json:"password"`
	From     string                                 `json:"from"`
	To       []string                               `json:"to"`
	CC       []string                               `json:"cc"`
	Subject  *func(ip string, reason string) string `json:"-"` // default: "[IP Sentry] IP {ip} has been banned"
	Body     *func(ip string, reason string) string `json:"-"` // default: "[IP Sentry] IP {ip} has been banned for {reason}"
}
