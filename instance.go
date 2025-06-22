package redisFallback

import (
	"context"
	"fmt"
	"net/smtp"
	"os"
	"strings"
	"time"

	goLogger "github.com/pardnchiu/go-logger"
	"github.com/redis/go-redis/v9"
)

func New(c Config) (*RedisFallback, error) {
	c.Log = validLoggerConfig(c)
	c.Option = validOptionData(c)

	logger, err := goLogger.New(c.Log)
	if err != nil {
		return nil, fmt.Errorf("Failed to initialize `pardnchiu/go-logger`: %w", err)
	}

	// * Initialize Redis
	redisClient := initRedis(c)

	ctx := context.Background()
	redisFallback := &RedisFallback{
		config:  c,
		logger:  logger,
		redis:   redisClient,
		context: ctx,
		writer: &Writer{
			config:  c,
			logger:  logger,
			queue:   make(chan WriteRequest, c.Option.MaxQueue),
			timer:   time.NewTicker(c.Option.TimeToWrite),
			pending: make(map[string]interface{}),
		},
	}

	// * check Redis connection
	if err := redisClient.Ping(ctx).Err(); err != nil {
		// * fallback mode
		logger.Error(err, "Failed to connect, Starting fallback mode")
		redisFallback.changeToFallbackMode()
	} else {
		// * normal mode
		logger.Info("Starting normal mode")
		redisFallback.changeToNormalMode()
	}

	go redisFallback.writer.start()
	go redisFallback.startMemoryCleanup()

	return redisFallback, nil
}

func initRedis(c Config) *redis.Client {
	if c.Redis == nil {
		c.Redis = &Redis{
			Host:     "localhost",
			Port:     6379,
			Password: "",
			DB:       0,
		}
	}
	if c.Redis.Host == "" {
		c.Redis.Host = "localhost"
	}
	if c.Redis.Port <= 0 || c.Redis.Port > 65535 {
		c.Redis.Port = 6379
	}
	if c.Redis.DB < 0 || c.Redis.DB > 15 {
		c.Redis.DB = 0
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", c.Redis.Host, c.Redis.Port),
		Password: c.Redis.Password,
		DB:       c.Redis.DB,
	})
	return redisClient
}

func (rf *RedisFallback) removeJSONFile(key string) {
	path := getPath(rf.config, key)
	os.Remove(path.filepath)
}

func (rf *RedisFallback) Close() {
	if rf.checker != nil {
		rf.checker.Stop()
	}
	rf.writer.timer.Stop()
	rf.redis.Close()
}

func (m *RedisFallback) sendEmail(ip string, reason string) {
	if m.config.Email == nil {
		return
	}

	subject := fmt.Sprintf("[IP Sentry] IP %s has been banned", ip)
	if m.config.Email.Subject != nil {
		str := (*m.config.Email.Subject)(ip, reason)
		if str != "" {
			subject = str
		}
	}
	body := fmt.Sprintf("[IP Sentry] IP %s has been banned for %s", ip, reason)
	if m.config.Email.Body != nil {
		str := (*m.config.Email.Body)(ip, reason)
		if str != "" {
			body = str
		}
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nCc: %s\r\nSubject: %s\r\n\r\n%s",
		m.config.Email.From,
		strings.Join(m.config.Email.To, ","),
		strings.Join(m.config.Email.CC, ","),
		subject,
		body)

	auth := smtp.PlainAuth("", m.config.Email.Username, m.config.Email.Password, m.config.Email.Host)
	addr := fmt.Sprintf("%s:%d", m.config.Email.Host, m.config.Email.Port)

	err := smtp.SendMail(addr, auth, m.config.Email.From, m.config.Email.To, []byte(msg))
	if err != nil {
		m.logger.Error(err, "Failed to send email")
	}
}

func validLoggerConfig(c Config) *Log {
	if c.Log == nil {
		c.Log = &Log{
			Path:    defaultLogPath,
			Stdout:  false,
			MaxSize: defaultLogMaxSize,
		}
	}
	if c.Log.Path == "" {
		c.Log.Path = defaultLogPath
	}
	if c.Log.MaxSize <= 0 {
		c.Log.MaxSize = defaultLogMaxSize
	}
	if c.Log.MaxBackup <= 0 {
		c.Log.MaxBackup = defaultLogMaxBackup
	}
	return c.Log
}

func validOptionData(c Config) *Options {
	if c.Option == nil {
		c.Option = &Options{
			DBPath:   defaultDBPath,
			MaxRetry: defaultMaxRetry,
		}
	}
	if c.Option.DBPath == "" {
		c.Option.DBPath = defaultDBPath
	}
	if c.Option.MaxRetry <= 0 {
		c.Option.MaxRetry = defaultMaxRetry
	}
	if c.Option.MaxQueue <= 0 {
		c.Option.MaxQueue = defaultMaxQueue
	}
	if c.Option.TimeToWrite <= 0 {
		c.Option.TimeToWrite = defaultTimeToWrite
	}
	if c.Option.TimeToCheck <= 0 {
		c.Option.TimeToCheck = defaultTimeToCheck
	}
	return c.Option
}
