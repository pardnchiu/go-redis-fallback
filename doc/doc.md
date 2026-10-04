# go-redis-fallback - Documentation

Last updated: 2026-10-05

> Back to [README](../README.md)

## Prerequisites

- Go 1.24.3 or higher (per `go.mod`)
- A Redis server (it may become unreachable at runtime; the local tiers take over)
- A writable local filesystem for `Options.DBPath` and `Log.Path`

## Installation

### Using go get

```bash
go get github.com/pardnchiu/go-redis-fallback
```

### From Source

```bash
git clone https://github.com/pardnchiu/go-redis-fallback.git
cd go-redis-fallback
go test ./...
```

## Configuration

Pass every setting to `New()` through `Config`; empty fields fall back to defaults.

### Redis

`Config.Redis` is required (pass `&redisFallback.Redis{}` even for all defaults); when nil, `New` panics after a successful ping.

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `Host` | No | `localhost` | Redis host |
| `Port` | No | `6379` | Port; values outside `1–65535` revert to the default |
| `Password` | No | `""` | Redis password |
| `DB` | No | `0` | Database index; values outside `0–15` revert to the default. Also names the local file subdirectory |

### Options

| Field | Default | Effect |
|-------|---------|--------|
| `DBPath` | `./files/redisFallback/db` | Root directory for fallback JSON files |
| `MaxRetry` | `3` | Retry count for Redis reads and writes; exhausting it switches to fallback mode |
| `MaxQueue` | `1000` | Fallback write queue length; a full queue triggers a synchronous file write |
| `TimeToWrite` | `3s` | Batch file-write interval in fallback mode |
| `TimeToCheck` | `1m` | Health-check (ping) interval in fallback mode, i.e. the upper bound on recovery detection latency |

### Log (inherited from `pardnchiu/go-logger`)

| Field | Default | Description |
|-------|---------|-------------|
| `Path` | `./logs/redisFallback` | Log directory |
| `Stdout` | `false` | Also write to stdout |
| `MaxSize` | `16 MiB` | Max size per log file |
| `MaxBackup` | `5` | Number of backup files |
| `Type` | `text` | `text` or `json` |

### Email

`Config` defines an `EmailConfig` type, but the current version never sends mail; setting it has no effect.

## Usage

### Basic

```go
package main

import (
	"fmt"
	"log"
	"time"

	redisFallback "github.com/pardnchiu/go-redis-fallback"
)

func main() {
	rf, err := redisFallback.New(redisFallback.Config{
		Redis: &redisFallback.Redis{
			Host: "localhost",
			Port: 6379,
			DB:   0,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer rf.Close()

	// Write with a 10-minute TTL
	if err := rf.Set("user:1", "pardn", 10*time.Minute); err != nil {
		log.Println(err)
	}

	// Read: falls back to memory / local files when Redis is unreachable
	value, err := rf.Get("user:1")
	if err != nil {
		log.Println(err)
		return
	}
	fmt.Println(value)

	if err := rf.Del("user:1"); err != nil {
		log.Println(err)
	}
}
```

### Read Fallback Flow

`Get` takes one of two paths based on the health state determined at startup or by the previous operation.

**Normal mode**

| Step | Condition | Behavior |
|------|-----------|----------|
| 1 | Memory cache hit, not expired | Return immediately and write the value back to Redis in a background goroutine (read-repair) |
| 2 | Memory cache hit, expired | Delete the memory entry and the local JSON file, return a `Not found` error |
| 3 | Memory cache miss | `GET` from Redis up to `MaxRetry` times; on a successful parse, store in memory and return |
| 4 | Retries exhausted | Switch to fallback mode (start the health checker) and continue on the fallback path below within the same call |

**Fallback mode**

| Step | Condition | Behavior |
|------|-----------|----------|
| 1 | Memory cache hit, not expired | Return immediately |
| 2 | Memory cache hit, expired | Delete the memory entry, return a `Not found` error (the background sweep removes the file) |
| 3 | Memory cache miss | Read `{DBPath}/{DB}/{md5[0:2]}/{md5[2:4]}/{md5[4:6]}/{md5}.json` |
| 4 | File exists, not expired | Backfill the memory cache and return |
| 5 | File expired | Delete the file, return a `Not found` error |
| 6 | File missing | Return a `Not found` error; a malformed file returns `Failed to parse` |

> [!IMPORTANT]
> Step 3 of normal mode only accepts JSON values matching the `Cache` structure (`key` / `data` / `type` / `timestamp` / `ttl`). A missing key (`redis.Nil`) or a value in any other format also counts as a failure and switches to fallback mode once retries are exhausted.

> [!NOTE]
> In normal mode the memory cache takes precedence over Redis: for any key written in the same process, `Get` returns the in-memory value even if the key was deleted or modified externally in Redis.

### Recovery Flow

1. Fallback mode starts a ticker at `TimeToCheck` intervals that sends `PING` to Redis on each tick.
2. Once `PING` succeeds, the ticker stops and recovery runs:
   - Walk every `.json` file under `{DBPath}/{DB}`, parse it, and load it into the memory cache (files that fail to read or parse are logged and skipped).
   - Iterate the memory cache and pipeline entries to Redis in batches of 100, setting the Redis TTL to "original TTL − elapsed time"; entries with remaining TTL ≤ 0 are not written.
   - Delete all local JSON files and empty directories.
   - Set the health state back to normal.
3. `isRecovering` uses CAS so only one resync runs at a time.

> [!NOTE]
> Entries without a TTL (`ttl = 0`) are skipped by the recovery batch because their remaining TTL is ≤ 0, and stay in the memory cache. When `Get` later hits them in normal mode, read-repair writes them back to Redis with no expiration.

`New()` runs the same recovery flow when its startup ping succeeds, so JSON files left by a previous process during fallback are resynced on the next start.

> [!WARNING]
> The first recovery step walks `{DBPath}/{DB}`; when that directory does not exist it returns an error before the health state is set to normal, and no health checker starts. On a first start (directory not yet created) the instance therefore stays in local mode even when the ping succeeds: reads and writes only touch memory and local files, never Redis. The log still prints `Starting normal mode`, and the error log records `Failed to search folder`.

### Advanced: Simulating a Redis Outage

```go
package main

import (
	"fmt"
	"log"
	"time"

	redisFallback "github.com/pardnchiu/go-redis-fallback"
)

func main() {
	rf, err := redisFallback.New(redisFallback.Config{
		Redis: &redisFallback.Redis{Host: "localhost", Port: 6380}, // no Redis listening on this port
		Log:   &redisFallback.Log{Path: "./logs/demo", Stdout: true},
		Option: &redisFallback.Options{
			DBPath:      "./files/demo",
			MaxRetry:    2,
			TimeToWrite: 1 * time.Second,
			TimeToCheck: 10 * time.Second,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer rf.Close()

	// Fallback mode: store in memory and enqueue for batch file write
	if err := rf.Set("session:abc", map[string]any{"uid": 1}, time.Hour); err != nil {
		log.Fatal(err)
	}

	// Wait for the batch file write (TimeToWrite)
	time.Sleep(2 * time.Second)

	// Memory cache hit
	value, err := rf.Get("session:abc")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(value)
}
```

Values restored from local JSON files are decoded by `encoding/json`: numbers become `float64` and objects become `map[string]any`, which differ from the original Go types at write time.

## API Reference

### New

```go
func New(c Config) (*RedisFallback, error)
```

Creates an instance and pings Redis: on success it runs the recovery flow and enters normal mode (not when `{DBPath}/{DB}` is missing; see the warning under Recovery Flow); on failure it enters fallback mode and starts the health checker. It returns an error only when the logger fails to initialize; an unreachable Redis is not an error.

### Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `Get` | `Get(key string) (interface{}, error)` | Reads per the Read Fallback Flow; returns an error when missing or expired |
| `Set` | `Set(key string, value interface{}, ttl time.Duration) error` | Normal mode writes to Redis (switching to fallback once retries are exhausted); fallback mode writes to memory and enqueues a batch file write. `ttl ≤ 0` means no expiry |
| `Del` | `Del(key string) error` | Removes the memory entry and the local file; also deletes the Redis key in normal mode |
| `Close` | `Close()` | Stops the health-check and write tickers and closes the Redis connection; queued writes not yet flushed are discarded |

### Types

| Type | Description |
|------|-------------|
| `Config` | Four groups: `Redis`, `Log`, `Option`, `Email` |
| `Redis` | Connection settings |
| `Options` | Fallback and recovery parameters |
| `Log` / `Logger` | Type aliases from `pardnchiu/go-logger` |
| `Cache` | Data structure for local JSON files and the memory cache: `Key`, `Data`, `Type`, `Timestamp`, `TTL` |
| `EmailConfig` | Notification settings (currently inactive) |

***

©️ 2025 [邱敬幃 Pardn Chiu](https://www.linkedin.com/in/pardnchiu)
