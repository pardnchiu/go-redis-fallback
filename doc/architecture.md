# go-redis-fallback - Architecture

Last updated: 2026-10-05

> Back to [README](../README.md)

## Overview

```mermaid
graph TB
    Client[Caller] --> API[Get / Set / Del]
    API --> Health{isHealth}
    Health -->|Normal| Mem[Memory Cache sync.Map]
    Health -->|Normal| Redis[(Redis)]
    Health -->|Fallback| Mem
    Mem -->|Fallback miss| File[Local JSON Files]
    API -->|Fallback write| Writer[Batch File Writer]
    Writer --> File
    Redis -->|Retries exhausted| Checker[Health-Check Ticker]
    Checker -->|Ping OK| Recovery[Recovery Flow]
    Recovery --> File
    Recovery --> Redis
    Sweeper[30s Expiry Sweep] --> Mem
    Sweeper --> File
```

## Module: Read Path (get.go)

Routes by health state; normal mode is memory-first with read-repair, and degrades within the same call on failure.

```mermaid
graph TB
    subgraph Get
        Start[Get key] --> Lock[RLock read isHealth]
        Lock -->|true| R1{Memory hit?}
        Lock -->|false| F1{Memory hit?}

        R1 -->|Hit, expired| R1x[Delete memory + file → Not found]
        R1 -->|Hit, valid| R1ok[go syncToRedis write-back]
        R1ok --> Ret[Return Data]
        R1 -->|Miss| R2[redis.Get loop MaxRetry times]
        R2 -->|OK and parses as Cache| R2ok[Store in memory]
        R2ok --> Ret
        R2 -->|All failed| R3[Lock → changeToFallbackMode → Unlock]
        R3 --> F1

        F1 -->|Hit, expired| F1x[Delete memory → Not found]
        F1 -->|Hit, valid| Ret
        F1 -->|Miss| F2[loadFromFile]
        F2 -->|File missing| F2x[Not found]
        F2 -->|Parse error| F2p[Failed to parse]
        F2 -->|Expired| F2e[Delete file → Not found]
        F2 -->|Valid| F2ok[Backfill memory]
        F2ok --> Ret
    end
```

## Module: Recovery (sync.go)

Once the health check detects Redis is back, local files are resynced to Redis through memory and local state is cleared.

```mermaid
graph TB
    subgraph changeToFallbackMode
        FB[isHealth = false] --> Has{checker exists?}
        Has -->|Yes| Skip[Return]
        Has -->|No| Tick[NewTicker TimeToCheck]
        Tick --> Ping{Ping}
        Ping -->|Fail| Tick
        Ping -->|OK| Stop[Stop and clear checker]
    end

    subgraph changeToNormalMode
        Walk[Walk .json under DBPath/DB] --> Load[Parse and store in memory cache]
        Load --> CAS{isRecovering CAS}
        CAS -->|Already running| Skip2[Skip resync]
        CAS -->|Acquired| Pipe[Pipeline over memory]
        Pipe --> TTL{Remaining TTL > 0?}
        TTL -->|Yes| Set[pipe.Set with remaining TTL]
        TTL -->|No| Next[Skip]
        Set --> Batch[Exec every 100 entries]
        Batch --> Clean[Delete JSON files and empty dirs]
        Skip2 --> Clean
        Clean --> OK[isHealth = true]
    end

    Stop -->|go| Walk
    New[New startup Ping OK] --> Walk
```

## Module: Write Path and File Layout (set.go / writer.go / unit.go)

Fallback writes land in memory first, then merge through a queue into batched file writes; file paths are sharded three levels deep by the key's MD5.

```mermaid
graph TB
    subgraph Set
        S[Set key value ttl] --> SH{isHealth}
        SH -->|true| SR[redis.Set retry MaxRetry]
        SR -->|OK| SM[Store in memory]
        SR -->|Fail| SF[Switch to Fallback]
        SF --> MM
        SH -->|false| MM[Store in memory]
        MM --> Q{Queue has room?}
        Q -->|Yes| Enq[Send to queue]
        Q -->|No| Sync[Synchronous writeToFile]
    end

    subgraph Writer
        Enq --> Pending[pending map merged by key]
        Timer[TimeToWrite Ticker] --> Flush[Copy and reset pending]
        Pending --> Flush
        Flush --> Par[Parallel writeToFile per key]
    end

    subgraph FileLayout[File Layout]
        Par --> P["DBPath/DB/md5[0:2]/md5[2:4]/md5[4:6]/md5.json"]
        Sync --> P
    end
```

## Data Flow

The full sequence from a Redis outage, through read fallback, to read-repair after recovery.

```mermaid
sequenceDiagram
    participant C as Caller
    participant RF as RedisFallback
    participant M as Memory Cache
    participant R as Redis
    participant F as Local JSON Files
    participant H as Health Checker

    C->>RF: Get(key) (normal mode)
    RF->>M: Load(key)
    M-->>RF: Miss
    loop MaxRetry times
        RF->>R: GET key
        R--xRF: Error
    end
    RF->>H: changeToFallbackMode starts ticker
    RF->>M: Load(key)
    M-->>RF: Miss
    RF->>F: Read MD5-sharded path
    F-->>RF: Cache JSON
    RF->>M: Store(key) backfill
    RF-->>C: Data

    loop Every TimeToCheck
        H->>R: PING
    end
    R-->>H: PONG
    H->>RF: go changeToNormalMode
    RF->>F: Walk all .json
    F-->>RF: Cache entries
    RF->>M: Store all
    RF->>R: Pipeline SET (remaining TTL, every 100)
    RF->>F: Delete files and empty dirs
    Note over RF: isHealth = true

    C->>RF: Get(key) (normal mode)
    RF->>M: Load(key)
    M-->>RF: Hit
    RF-->>C: Data
    RF-)R: go syncToRedis (read-repair)
```

## State Machine

```mermaid
stateDiagram-v2
    [*] --> Init
    Init --> Recovering: Startup ping OK
    Init --> Fallback: Startup ping failed
    Normal --> Fallback: Get/Set fail MaxRetry times
    Fallback --> Fallback: Health-check ping failed
    Fallback --> Recovering: Health-check ping OK
    Recovering --> Normal: Load files → resync Redis → remove files
    Recovering --> LocalOnly: DBPath/DB directory missing
    LocalOnly --> [*]: Close
    Normal --> [*]: Close
    Fallback --> [*]: Close
```

***

©️ 2025 [邱敬幃 Pardn Chiu](https://www.linkedin.com/in/pardnchiu)
