package redisFallback

import (
	"context"
	"fmt"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func New(c Config) (*RedisFallback, error) {
	// * Initialize logger
	logger, err := initLogger(c)
	if err != nil {
		return nil, err
	}

	// * Initialize Redis
	redisClient := initRedis(c)

	// * Validate options
	c = validateOptions(c)

	// * Create fallback db directory
	if err := os.MkdirAll(c.Options.DBPath, 0755); err != nil {
		logger.error(err, "Failed to create DB folder")
	}

	ctx := context.Background()
	redisFallback := &RedisFallback{
		config:  c,
		logger:  logger,
		redis:   redisClient,
		context: ctx,
		writer: &Writer{
			config:  c,
			logger:  logger,
			queue:   make(chan WriteRequest, c.Options.MaxQueue),
			timer:   time.NewTicker(c.Options.TimeToWrite),
			pending: make(map[string]interface{}),
		},
	}

	// * check Redis connection
	if err := redisClient.Ping(ctx).Err(); err != nil {
		// * fallback mode
		logger.error(err, "Failed to connect, Starting fallback mode")
		redisFallback.changeToFallbackMode()
	} else {
		// * normal mode
		logger.info("Starting normal mode")
		redisFallback.changeToNormalMode()
	}

	go redisFallback.writer.start()
	go redisFallback.startMemoryCleanup()

	return redisFallback, nil
}

func initLogger(c Config) (*Logger, error) {
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

	logger, err := newLogger(c.Log)
	if err != nil {
		return nil, fmt.Errorf("Can not initialize logger: %v", err)
	}
	return logger, nil
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

func validateOptions(c Config) Config {
	if c.Options == nil {
		c.Options = &Options{
			DBPath:   defaultDBPath,
			MaxRetry: defaultMaxRetry,
		}
	}
	if c.Options.DBPath == "" {
		c.Options.DBPath = defaultDBPath
	}
	if c.Options.MaxRetry <= 0 {
		c.Options.MaxRetry = defaultMaxRetry
	}
	if c.Options.MaxQueue <= 0 {
		c.Options.MaxQueue = defaultMaxQueue
	}
	if c.Options.TimeToWrite <= 0 {
		c.Options.TimeToWrite = defaultTimeToWrite
	}
	if c.Options.TimeToCheck <= 0 {
		c.Options.TimeToCheck = defaultTimeToCheck
	}
	return c
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
		m.logger.error(err, "Failed to send email")
	}
}
