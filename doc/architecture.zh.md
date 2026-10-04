# go-redis-fallback - 架構

最後更新：2026-10-05

> 返回 [README](./README.zh.md)

## 概覽

```mermaid
graph TB
    Client[呼叫端] --> API[Get / Set / Del]
    API --> Health{isHealth}
    Health -->|正常| Mem[記憶體快取 sync.Map]
    Health -->|正常| Redis[(Redis)]
    Health -->|Fallback| Mem
    Mem -->|Fallback 未命中| File[本地 JSON 檔]
    API -->|Fallback 寫入| Writer[批次寫檔 Writer]
    Writer --> File
    Redis -->|重試用盡| Checker[健康檢查 Ticker]
    Checker -->|Ping 成功| Recovery[恢復流程]
    Recovery --> File
    Recovery --> Redis
    Sweeper[30 秒過期掃除] --> Mem
    Sweeper --> File
```

## Module: 讀取路徑（get.go）

依健康狀態分流，正常模式以記憶體優先並 read-repair，失敗時於同一次呼叫內降級。

```mermaid
graph TB
    subgraph Get
        Start[Get key] --> Lock[RLock 讀取 isHealth]
        Lock -->|true| R1{記憶體命中?}
        Lock -->|false| F1{記憶體命中?}

        R1 -->|命中且過期| R1x[刪記憶體 + 刪檔 → Not found]
        R1 -->|命中且有效| R1ok[go syncToRedis 回寫]
        R1ok --> Ret[回傳 Data]
        R1 -->|未命中| R2[redis.Get 迴圈 MaxRetry 次]
        R2 -->|成功且可解析為 Cache| R2ok[寫入記憶體]
        R2ok --> Ret
        R2 -->|全部失敗| R3[Lock → changeToFallbackMode → Unlock]
        R3 --> F1

        F1 -->|命中且過期| F1x[刪記憶體 → Not found]
        F1 -->|命中且有效| Ret
        F1 -->|未命中| F2[loadFromFile]
        F2 -->|檔案不存在| F2x[Not found]
        F2 -->|解析失敗| F2p[Failed to parse]
        F2 -->|已過期| F2e[刪檔 → Not found]
        F2 -->|有效| F2ok[回填記憶體]
        F2ok --> Ret
    end
```

## Module: 恢復（sync.go）

健康檢查偵測 Redis 恢復後，將本地檔案經記憶體回灌至 Redis 並清除本地狀態。

```mermaid
graph TB
    subgraph changeToFallbackMode
        FB[isHealth = false] --> Has{checker 已存在?}
        Has -->|是| Skip[直接返回]
        Has -->|否| Tick[NewTicker TimeToCheck]
        Tick --> Ping{Ping}
        Ping -->|失敗| Tick
        Ping -->|成功| Stop[停止並清空 checker]
    end

    subgraph changeToNormalMode
        Walk[走訪 DBPath/DB 的 .json] --> Load[解析並寫入記憶體快取]
        Load --> CAS{isRecovering CAS}
        CAS -->|已在執行| Skip2[略過回灌]
        CAS -->|取得| Pipe[Pipeline 遍歷記憶體]
        Pipe --> TTL{剩餘 TTL > 0?}
        TTL -->|是| Set[pipe.Set 剩餘 TTL]
        TTL -->|否| Next[略過]
        Set --> Batch[每 100 筆 Exec]
        Batch --> Clean[刪除 JSON 檔與空目錄]
        Skip2 --> Clean
        Clean --> OK[isHealth = true]
    end

    Stop -->|go| Walk
    New[New 啟動 Ping 成功] --> Walk
```

## Module: 寫入與檔案佈局（set.go / writer.go / unit.go）

Fallback 寫入先進記憶體，再經佇列合併後批次落檔；檔案路徑以 key 的 MD5 三層分片。

```mermaid
graph TB
    subgraph Set
        S[Set key value ttl] --> SH{isHealth}
        SH -->|true| SR[redis.Set 重試 MaxRetry]
        SR -->|成功| SM[寫入記憶體]
        SR -->|失敗| SF[切入 Fallback]
        SF --> MM
        SH -->|false| MM[寫入記憶體]
        MM --> Q{佇列有空位?}
        Q -->|是| Enq[送入 queue]
        Q -->|否| Sync[同步 writeToFile]
    end

    subgraph Writer
        Enq --> Pending[pending map 依 key 合併]
        Timer[TimeToWrite Ticker] --> Flush[複製並清空 pending]
        Pending --> Flush
        Flush --> Par[每 key 並行 writeToFile]
    end

    subgraph 檔案路徑
        Par --> P["DBPath/DB/md5[0:2]/md5[2:4]/md5[4:6]/md5.json"]
        Sync --> P
    end
```

## 資料流

Redis 失聯、讀取降級，至恢復後 read-repair 的完整序列。

```mermaid
sequenceDiagram
    participant C as 呼叫端
    participant RF as RedisFallback
    participant M as 記憶體快取
    participant R as Redis
    participant F as 本地 JSON 檔
    participant H as 健康檢查

    C->>RF: Get(key)（正常模式）
    RF->>M: Load(key)
    M-->>RF: 未命中
    loop MaxRetry 次
        RF->>R: GET key
        R--xRF: 錯誤
    end
    RF->>H: changeToFallbackMode 啟動 Ticker
    RF->>M: Load(key)
    M-->>RF: 未命中
    RF->>F: 讀取 md5 分片路徑
    F-->>RF: Cache JSON
    RF->>M: Store(key) 回填
    RF-->>C: Data

    loop 每 TimeToCheck
        H->>R: PING
    end
    R-->>H: PONG
    H->>RF: go changeToNormalMode
    RF->>F: 走訪所有 .json
    F-->>RF: Cache 項目
    RF->>M: Store 全部
    RF->>R: Pipeline SET（剩餘 TTL，每 100 筆）
    RF->>F: 刪除檔案與空目錄
    Note over RF: isHealth = true

    C->>RF: Get(key)（正常模式）
    RF->>M: Load(key)
    M-->>RF: 命中
    RF-->>C: Data
    RF-)R: go syncToRedis（read-repair）
```

## 狀態機

```mermaid
stateDiagram-v2
    [*] --> 初始化
    初始化 --> 恢復中: 啟動 Ping 成功
    初始化 --> Fallback: 啟動 Ping 失敗
    正常 --> Fallback: Get/Set 重試 MaxRetry 次皆失敗
    Fallback --> Fallback: 健康檢查 Ping 失敗
    Fallback --> 恢復中: 健康檢查 Ping 成功
    恢復中 --> 正常: 載入檔案 → 回灌 Redis → 清除檔案
    恢復中 --> 本地模式無健康檢查: DBPath/DB 目錄不存在
    本地模式無健康檢查 --> [*]: Close
    正常 --> [*]: Close
    Fallback --> [*]: Close
```

***

©️ 2025 [邱敬幃 Pardn Chiu](https://www.linkedin.com/in/pardnchiu)
