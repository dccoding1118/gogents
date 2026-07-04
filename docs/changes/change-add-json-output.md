# CH-004 — runtime 觀測指令支援 `--json` 結構化輸出

## 1. 背景與動機

`agent-manager`（am）的 gogents 轉接器（`internal/gogents`）需以程式讀取 gogents 的 runtime 狀態來推進其 reconciler。現行 `runtime list` / `runtime status` 僅有**給人看的文字輸出**（tab 分隔列、`key: value` 區塊），am 只能逐行解析——這等於把 am 綁在 gogents 的顯示層：任何欄位增減、順序調整或時間格式變動都會無聲破壞解析。

本變更為 `runtime list` / `runtime status` 加上 opt-in 的 `--json`，提供**穩定的資料契約**，並在 `--json` 模式下以結構化錯誤（含 `code`）表達失敗，讓 am 能可靠區分「target 未註冊」與其他失敗。

## 2. 變更目標

1. `gogents runtime list --json`：輸出 runtime 陣列 JSON。
2. `gogents runtime status <target> --json`：輸出單一 runtime 狀態 JSON。
3. `--json` 模式下，失敗以結構化錯誤物件（含 `code`）輸出至 stderr、非零 exit。
4. 不加 `--json` 時，現行文字輸出**完全不變**（向後相容）。
5. `logs` 不在本次範圍（維持原始 JSONL 直通）。

## 3. 具體需求規格

### 3.1 `runtime list --json`

- 成功：stdout 輸出 JSON 陣列、exit 0。無註冊時輸出 `[]`（**非**文字 `(no registered runtimes)`）。
- 每筆對應現行 `RuntimeView`：

  ```json
  [
    {"name": "coding", "workspace": "/home/u/.am/workers/coding", "registered_at": "2026-07-04T12:00:00+08:00"}
  ]
  ```

| JSON 欄位 | 來源 | 說明 |
|---|---|---|
| `name` | `RuntimeView.Name` | runtime 名 |
| `workspace` | `RuntimeView.Workspace` | workspace 路徑（註冊主鍵） |
| `registered_at` | `RuntimeView.RegisteredAt` | 註冊時間，RFC3339 |

### 3.2 `runtime status <target> --json`

- 成功：stdout 輸出單一 JSON 物件、exit 0。對應現行 `StatusView`：

  ```json
  {
    "name": "coding",
    "workspace": "/home/u/.am/workers/coding",
    "registered_at": "2026-07-04T12:00:00+08:00",
    "runtime_state": "idle",
    "pid": 12345,
    "alive": true,
    "current_task": "t-0007",
    "last_sync_at": "2026-07-04T12:03:00+08:00",
    "last_sync_status": "ok"
  }
  ```

| JSON 欄位 | 來源 | 說明 |
|---|---|---|
| `name` / `workspace` / `registered_at` | `RuntimeView` | 同上 |
| `runtime_state` | `StatusView.RuntimeState` | `idle/executing/verifying/reporting/failed/broken`；pid 不存活時已為 `down` |
| `pid` | `StatusView.Pid` | 記錄的 daemon PID |
| `alive` | `StatusView.Alive` | 查詢時 pid 檢核結果 |
| `current_task` | `StatusView.CurrentTask` | 當前任務 id，空字串＝無 |
| `last_sync_at` | `StatusView.LastSyncAt` | RFC3339；尚未同步時輸出空字串 `""` |
| `last_sync_status` | `StatusView.LastSyncStatus` | 最近同步結果 |

- 時間戳一律 RFC3339（`StatusView.LastSyncAt` 為 `time.Time`，marshal 時自然輸出；零值輸出空字串，不輸出 `0001-01-01…`）。

### 3.3 `--json` 模式的結構化錯誤

- 失敗時 stdout 不輸出成功 payload；stderr 輸出單一 JSON 錯誤物件、非零 exit：

  ```json
  {"error": {"code": "runtime_not_found", "message": "runtime \"coding\" not registered"}}
  ```

| `code` | 情境 |
|---|---|
| `runtime_not_found` | `Resolve` 失敗：target 不在 `~/.gogents/config.yaml` 註冊表。 |
| `state_not_found` | 已註冊但無 `state.yaml`（尚未 `start` 過）。 |
| `internal` | 其他失敗（load 錯誤等）。 |

- exit code 維持現行（非零即可），**分類以 `error.code` 表達**，不新增 exit code 方案。
- 非 `--json` 模式的錯誤輸出（stderr `error: …` 文字）維持不變。

### 3.4 旗標行為

- `--json` 為 `runtime list` 與 `runtime status` 各自的布林旗標。
- 位置容忍度比照現行 flag 解析（`runtime status <target> --json` 與 `runtime status --json <target>` 皆可），與 `logs --error` 一致風格。

## 4. 交付物

- `internal/cli/`：`runtime list` / `runtime status` 加 `--json` 旗標與 JSON 輸出分支；`--json` 錯誤物件輸出。
- JSON 輸出型別（可直接對現有 `RuntimeView` / `StatusView` 加 JSON tag，或另立 DTO；`last_sync_at` 需自訂為 RFC3339 字串以處理零值空字串）。
- 對應單元測試：list（多筆／空陣列）、status（含 `down`／空欄位）、錯誤物件（各 `code`）。
- 更新 `docs/designs/` 對應觀測介面設計文件。

## 5. 相容性

- `--json` 為 opt-in；未帶時所有現行文字輸出與 exit 行為不變，CH-002/CH-003 流程與人工使用不受影響。
- `logs` 不變。

## 6. 關聯

- am 轉接器設計：`~/workspace/agent-manager/docs/designs/design-gogents-adapter.md`（消費本 JSON 契約）。
- am 依賴的 gogents 介面契約：`~/workspace/agent-manager/docs/design.md` §4.4。
- 對映現行實作：`internal/cli/query.go`（`RuntimeView` / `StatusView` / `List` / `Status`）、`internal/cli/cli.go`（`runRuntime`）。
