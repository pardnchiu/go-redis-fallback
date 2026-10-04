# go-redis-fallback - 技術文件

最後更新：2026-10-05

> 返回 [README](./README.zh.md)

## 前置需求

- Go 1.24.3 或以上（依 `go.mod`）
- Redis 伺服器（可在執行期間失聯；失聯時改由本地層服務）
- 對 `Options.DBPath` 與 `Log.Path` 具寫入權限的本地檔案系統

## 安裝

### 使用 go get

```bash
go get github.com/pardnchiu/go-redis-fallback
```

### 從原始碼

```bash
git clone https://github.com/pardnchiu/go-redis-fallback.git
cd go-redis-fallback
go test ./...
```

## 設定

所有設定透過 `Config` 傳入 `New()`，未填欄位套用預設值。

### Redis

`Config.Redis` 必須傳入（全用預設值也要傳 `&redisFallback.Redis{}`）；為 nil 時 `New` 在 Ping 成功後 panic。

| 欄位 | 必要 | 預設值 | 說明 |
|------|------|--------|------|
| `Host` | 否 | `localhost` | Redis 主機位址 |
| `Port` | 否 | `6379` | 連接埠，超出 `1–65535` 時回退預設值 |
| `Password` | 否 | `""` | Redis 密碼 |
| `DB` | 否 | `0` | 資料庫編號，超出 `0–15` 時回退預設值；同時決定本地檔案子目錄 |

### Options

| 欄位 | 預設值 | 影響範圍 |
|------|--------|----------|
| `DBPath` | `./files/redisFallback/db` | Fallback 本地 JSON 檔根目錄 |
| `MaxRetry` | `3` | 讀／寫 Redis 的重試次數，用盡即切入 fallback 模式 |
| `MaxQueue` | `1000` | Fallback 寫入佇列長度，佇列滿時改為同步寫檔 |
| `TimeToWrite` | `3s` | Fallback 模式下批次寫檔間隔 |
| `TimeToCheck` | `1m` | Fallback 模式下健康檢查（Ping）間隔，即恢復偵測延遲上限 |

### Log（繼承自 `pardnchiu/go-logger`）

| 欄位 | 預設值 | 說明 |
|------|--------|------|
| `Path` | `./logs/redisFallback` | 日誌目錄 |
| `Stdout` | `false` | 是否同時輸出至標準輸出 |
| `MaxSize` | `16 MiB` | 單檔大小上限 |
| `MaxBackup` | `5` | 備份檔數量 |
| `Type` | `text` | `text` 或 `json` |

### Email

`EmailConfig` 型別已定義於 `Config`，但目前版本不會觸發寄信；設定此欄位不影響行為。

## 使用方式

### 基礎

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

	// 寫入，TTL 10 分鐘
	if err := rf.Set("user:1", "pardn", 10*time.Minute); err != nil {
		log.Println(err)
	}

	// 讀取：Redis 失聯時自動改由記憶體／本地檔案回應
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

### 讀取降級流程

`Get` 依啟動時或上一次操作判定的健康狀態走兩條路徑：

**正常模式**

| 步驟 | 條件 | 行為 |
|------|------|------|
| 1 | 記憶體快取命中且未過期 | 立即回傳，並於背景 goroutine 將該值寫回 Redis（read-repair） |
| 2 | 記憶體快取命中但已過期 | 刪除記憶體項目與本地 JSON 檔，回傳 `Not found` 錯誤 |
| 3 | 記憶體未命中 | 向 Redis `GET`，最多 `MaxRetry` 次；成功解析即寫入記憶體快取並回傳 |
| 4 | 重試用盡 | 切入 fallback 模式（啟動健康檢查），同一次呼叫改走下方 fallback 路徑 |

**Fallback 模式**

| 步驟 | 條件 | 行為 |
|------|------|------|
| 1 | 記憶體快取命中且未過期 | 立即回傳 |
| 2 | 記憶體快取命中但已過期 | 刪除記憶體項目，回傳 `Not found` 錯誤（檔案交由背景掃除） |
| 3 | 記憶體未命中 | 讀取 `{DBPath}/{DB}/{md5[0:2]}/{md5[2:4]}/{md5[4:6]}/{md5}.json` |
| 4 | 檔案存在且未過期 | 回填記憶體快取並回傳 |
| 5 | 檔案已過期 | 刪除檔案，回傳 `Not found` 錯誤 |
| 6 | 檔案不存在 | 回傳 `Not found` 錯誤；格式錯誤回傳 `Failed to parse` |

> [!IMPORTANT]
> 正常模式的步驟 3 只接受符合 `Cache` 結構（`key`／`data`／`type`／`timestamp`／`ttl`）的 JSON 值。鍵不存在（`redis.Nil`）或值不是該格式時，同樣會被計為失敗並在重試用盡後切入 fallback 模式。

> [!NOTE]
> 正常模式下記憶體快取優先於 Redis：同一程序內寫入過的鍵，即使在 Redis 端被外部刪除或修改，`Get` 仍回傳記憶體中的值。

### 恢復流程

1. Fallback 模式啟動一個間隔為 `TimeToCheck` 的 Ticker，每次對 Redis 執行 `PING`。
2. `PING` 成功後停止 Ticker，執行恢復：
   - 走訪 `{DBPath}/{DB}` 下所有 `.json` 檔，解析後載入記憶體快取（讀取或解析失敗的檔案記錄錯誤後略過）。
   - 遍歷記憶體快取，以 Pipeline 每 100 筆一批寫回 Redis，Redis 端 TTL 設為「原 TTL − 已經過時間」；剩餘 TTL ≤ 0 的項目不寫入。
   - 刪除所有本地 JSON 檔與空目錄。
   - 將健康狀態設回正常。
3. `isRecovering` 以 CAS 保證同一時間只有一個回灌在執行。

> [!NOTE]
> 未設定 TTL（`ttl = 0`）的項目在恢復批次中會因剩餘 TTL ≤ 0 被略過，仍保留於記憶體快取；之後在正常模式下被 `Get` 命中時，由 read-repair 以無過期時間寫回 Redis。

`New()` 啟動時若 Ping 成功，同樣執行上述恢復流程，因此上一個程序在 fallback 期間留下的 JSON 檔會在下次啟動時回灌。

> [!WARNING]
> 恢復流程第一步走訪 `{DBPath}/{DB}` 時，若該目錄不存在會直接回傳錯誤，健康狀態不會被設為正常，也不會啟動健康檢查。首次啟動（目錄尚未建立）時，即使 Ping 成功，實例仍停留在本地模式：讀寫只走記憶體與本地檔案，不會存取 Redis。日誌仍會輸出 `Starting normal mode`，並在 error log 記錄 `Failed to search folder`。

### 進階：模擬 Redis 失聯

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
		Redis: &redisFallback.Redis{Host: "localhost", Port: 6380}, // 無 Redis 監聽的連接埠
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

	// Fallback 模式：寫入記憶體並排入批次寫檔佇列
	if err := rf.Set("session:abc", map[string]any{"uid": 1}, time.Hour); err != nil {
		log.Fatal(err)
	}

	// 等待批次寫檔（TimeToWrite）
	time.Sleep(2 * time.Second)

	// 記憶體命中
	value, err := rf.Get("session:abc")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(value)
}
```

從本地 JSON 檔還原的值經 `encoding/json` 解碼，數值為 `float64`、物件為 `map[string]any`，與寫入時的原始 Go 型別不同。

## API 參考

### New

```go
func New(c Config) (*RedisFallback, error)
```

建立實例並 Ping Redis：成功則執行恢復流程後進入正常模式（`{DBPath}/{DB}` 不存在時不會進入，見恢復流程的警告），失敗則進入 fallback 模式並啟動健康檢查。僅在 logger 初始化失敗時回傳錯誤，Redis 無法連線不視為錯誤。

### 方法

| 方法 | 簽章 | 說明 |
|------|------|------|
| `Get` | `Get(key string) (interface{}, error)` | 依「讀取降級流程」取值；不存在或過期回傳錯誤 |
| `Set` | `Set(key string, value interface{}, ttl time.Duration) error` | 正常模式寫 Redis（重試用盡切 fallback），fallback 模式寫記憶體並排入批次寫檔；`ttl ≤ 0` 表示不過期 |
| `Del` | `Del(key string) error` | 刪除記憶體項目與本地檔案；正常模式下同時刪除 Redis 鍵 |
| `Close` | `Close()` | 停止健康檢查與寫檔 Ticker，關閉 Redis 連線；尚未寫出的佇列項目不會被 flush |

### 型別

| 型別 | 說明 |
|------|------|
| `Config` | `Redis`、`Log`、`Option`、`Email` 四組設定 |
| `Redis` | 連線設定 |
| `Options` | 降級與恢復行為參數 |
| `Log` / `Logger` | `pardnchiu/go-logger` 的型別別名 |
| `Cache` | 本地 JSON 檔與記憶體快取的資料結構：`Key`、`Data`、`Type`、`Timestamp`、`TTL` |
| `EmailConfig` | 通知設定（目前未啟用） |

***

©️ 2025 [邱敬幃 Pardn Chiu](https://www.linkedin.com/in/pardnchiu)
