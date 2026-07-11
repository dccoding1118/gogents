# R09 - 日誌規範

> 主需求檔：[requirement.md](../requirement.md)

## 目的

定義 daemon 執行期間的日誌輸出規範,包含日誌檔案位置、格式、事件分類、欄位約定,作為其他需求(R04、R05、R06、R07、R08、R10)寫入日誌的共通基礎。

## 日誌檔案

每個 workspace 對應一組獨立的日誌檔案,位於:

```text
~/.gogents/logs/<workspace-hash>/
├── daemon.jsonl     # 一般執行日誌(全集)
└── error.jsonl      # 錯誤日誌(error 事件的複本)
```

全域家目錄根預設 `~/.gogents`,可經 `GOGENTS_HOME` 覆寫(見 R01);`logs/` 隨之移動。`<workspace-hash>` 為 workspace 絕對路徑的 SHA1 前 8 碼(見 R01)。

### 雙檔案分流

- **daemon.jsonl**: 記錄所有事件(info、warning、error 皆寫入),為完整日誌
- **error.jsonl**: 僅記錄 error 層級事件,作為快速檢視管道
- 所有 error 事件會**同時寫入兩個檔案**。daemon.jsonl 保留完整脈絡;error.jsonl 供使用者直接 tail 觀察是否有異常

### 同時輸出至 stdout/stderr

- daemon.jsonl 的內容同時寫入 stdout
- error.jsonl 的內容同時寫入 stderr(也就是 error 事件會進 stdout 一次、進 stderr 一次)
- 方便使用者用 `tail -f`、systemd journal、`task-syncer daemon | grep` 等方式即時觀察

## 日誌格式

採用 JSONL(每行一筆 JSON 物件),固定欄位如下:

```json
{
  "ts": "2026-04-07T18:05:00.123+08:00",
  "level": "info",
  "event": "sync.completed",
  "task_id": "github-42",
  "message": "sync completed: 2 new, 1 updated, 0 deleted",
  "fields": {
    "new_count": 2,
    "updated_count": 1,
    "deleted_count": 0,
    "duration_ms": 1234
  }
}
```

### 欄位定義

| 欄位      | 型別   | 必填 | 說明                                                     |
| --------- | ------ | ---- | -------------------------------------------------------- |
| `ts`      | string | 是   | ISO 8601 格式時間戳,**Asia/Taipei 時區(UTC+8)**,毫秒精度 |
| `level`   | string | 是   | `info` / `warning` / `error`                             |
| `event`   | string | 是   | 事件代號,採點分命名(見下方事件命名規範)                  |
| `task_id` | string | 否   | 與任務相關的事件必填,daemon 層級事件可省略               |
| `message` | string | 是   | 給人閱讀的簡短說明                                       |
| `fields`  | object | 否   | 結構化補充資訊,內容依 event 類型而定                     |

### 時區

- 所有時間戳使用 Asia/Taipei(UTC+8),格式範例:`2026-04-07T18:05:00.123+08:00`
- task.yaml 與 state.yaml 內的 timestamps 亦使用相同時區(建議 R03、R04 統一遵循)
- 使用者在多時區環境工作時若需要轉換,可用 `jq` 或外部工具後處理

## 事件命名規範

採用 `<domain>.<action>` 格式,domain 對應需求模組,action 描述發生的動作。

### Daemon 生命週期(R04)

- `daemon.start` — daemon 啟動成功
- `daemon.start_failed` — 啟動失敗(error)
- `daemon.shutdown` — 正常關閉
- `daemon.shutdown_forced` — 收到第二次信號強制退出(warning)
- `daemon.recover_processing` — 重啟時將 processing 任務重置為 queued(warning)
- `daemon.precheck_failed` — 啟動前置檢查失敗(error,來自 R02 前置檢查)

### Sync(R05)

- `sync.started` — 開始一次 sync 週期
- `sync.completed` — sync 成功結束(含摘要)
- `sync.failed` — sync 整體失敗(error)
- `sync.task_created` — 偵測到新任務並建立 task.yaml
- `sync.task_updated` — 既有任務狀態變化
- `sync.task_deleted` — 偵測到來源刪除,任務轉為 deleted
- `sync.failure_lock_reached` — 任務達 max_failures 不再重跑(warning,僅首次)

### 任務執行(R06、R07)

- `task.started` — 任務從 queued 轉為 processing
- `task.agent_invoked` — agent CLI 啟動
- `task.agent_exited` — agent CLI 結束(含 exit code)
- `task.completed` — 任務轉為 done(coding 與 reporting 皆成功)
- `task.failed` — 任務轉為 failed(coding 階段失敗,含失敗原因)
- `task.reporting_failed` — 任務轉為 reporting(coding 成功但 GitHub 回寫失敗,warning)
- `task.state_changed` — 一般狀態轉換事件

### GitHub 回報(R08)

- `github.comment_created`
- `github.comment_updated`
- `github.label_changed`
- `github.cli_failed` — gh 指令呼叫失敗(error 或 warning,視重試結果)

### 狀態檔案

- `state.write_failed` — task.yaml 或 state.yaml 寫入失敗(error)

## 層級使用準則

- **info**: 正常運作事件,如 sync 完成、任務完成、daemon 啟動
- **warning**: 非預期但 daemon 可繼續運作,如重啟恢復、達失敗門檻、單一 issue 處理失敗、task 轉為 reporting
- **error**: daemon 無法完成關鍵動作,如啟動失敗、整體 sync 失敗、檔案寫入失敗

每個寫入日誌的需求(R04~R08、R10)都應在其文件內標註使用的 event 名稱與層級。

## 不做的部分

目前版本刻意不實作以下功能,留待後續版本:

- **日誌輪替(rotation)**: 不做檔案切割,假設使用者用 logrotate 或手動清理
- **日誌保留策略**: 不自動刪除舊日誌
- **採樣與限流**: 所有事件都寫入,不做去重或限頻
- **遠端日誌轉發**: 不整合 syslog、Loki、ELK 等
- **結構化查詢工具**: 使用者可用 `jq`、`grep` 自行處理 JSONL

## 寫入失敗處理

日誌寫入本身失敗時:

- 嘗試寫入 stderr 一條 fallback 訊息
- 不中止 daemon 主流程
- 不重試寫入(避免無窮迴圈)

## 原子性與順序

- JSONL 採 append-only 寫入,每筆事件以 `\n` 結尾
- 同一個 daemon 行程內,日誌寫入應在單一 goroutine / mutex 保護下進行,避免多個 sync/task 並行造成行交錯
- 由於目前主迴圈是序列化的(見 R04),實際上不會有並行寫入的情況,但實作仍應保留鎖以防未來改動

## 相關需求

- R01 - Workspace 結構(日誌目錄位置、workspace-hash 計算)
- R02 - Runtime 設定檔(daemon.precheck_failed 事件對應啟動前置檢查)
- R04 - Daemon 生命週期(daemon.* 事件)
- R05 - GitHub Issue 同步(sync.* 事件)
- R06 - 任務執行流程(task.* 事件)
- R07 - 結果處理(task.completed / task.failed / task.reporting_failed)
- R08 - GitHub 狀態回報(github.* 事件)
- R10 - 錯誤處理(error 層級事件的整體策略)
