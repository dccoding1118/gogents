# R12 - Runtime 註冊與觀測

> 主需求檔：[requirement.md](../requirement.md)

## 目的

定義 gogents 的 runtime 註冊(`start` 寫入 `~/.gogents/config.yaml`)與觀測 CLI(`gogents runtime list`、`gogents runtime status`、`gogents logs`),使 agent-manager (am) 及使用者能列出已註冊 runtime、查詢單一 runtime 的即時執行狀態、匯出日誌供稽核。am 不維護執行狀態副本,一律經此介面向 gogents 查詢(單向 `am → gogents`)。

## Runtime 註冊

`gogents start` 於啟動時(如同開啟 logs)將本 runtime 註冊寫入 config.yaml(位於全域家目錄根,預設 `~/.gogents`,可經 `GOGENTS_HOME` 覆寫,見 R01;結構見 R02):

- **以 `workspace` 路徑為主鍵(upsert-by-workspace)**:workspace 目錄可能被檔案系統刪除、之後於同路徑重建新 runtime;以路徑為 key 才能覆蓋舊註冊,避免殘留
- 該 workspace 路徑已存在 → **覆蓋更新** `name` 與 `registered_at`;否則新增一筆
- **無 `active_runtime` 欄位**:daemon 由 `--workspace` 明確指定,不從 config.yaml 選擇
- 註冊寫入失敗僅記 error、不中止啟動(config.yaml 屬觀測輔助)

## runtime_state 與 pid 檢核

runtime 的執行狀態由兩部分構成:

- **`runtime_state`**(state.yaml 落檔,見 R04):`idle` / `executing` / `verifying` / `reporting` / `failed` / `broken`
- **`down`**(不落檔,查詢時推得):`runtime status` 讀 state.yaml 的 `daemon.pid`,檢核該 pid 是否存在;不存在即回報 `down`,即使 state.yaml 殘留舊的 `runtime_state`。此為死掉的 daemon 無法自我更新狀態的必要補償

## 觀測 CLI

### `gogents runtime list`

- 讀 `~/.gogents/config.yaml`,列出所有已註冊 runtime
- 每筆輸出:`workspace` / `name` / `registered_at`
- 不檢核 pid(僅列註冊表);逐筆 live 狀態由 `runtime status` 提供

### `gogents runtime status <workspace|name>`

- 參數接受 workspace 路徑或 runtime name;以 name 指定時經 config.yaml 反查 workspace(name 非唯一時以最近註冊者為準)
- 讀對應 workspace 的 state.yaml,回報:
  - `runtime_state`
  - `pid`
  - 當前任務(`current_task.task_id`)
  - 最近同步(`sync.last_sync_at` / `last_sync_status`)
- **查詢時檢核 pid**:`daemon.pid` 不存在則回報 `down`(覆蓋 state.yaml 殘留的舊 `runtime_state`)
- 找不到對應 runtime(未註冊、state.yaml 不存在)→ 明確錯誤

### `gogents logs <workspace|name>`

- 參數同 `runtime status`
- 輸出該 runtime 的 `~/.gogents/logs/<hash>/` 日誌(`daemon.jsonl` / `error.jsonl`)
- 供 am 稽核匯出併入(`am audit export` 經此取執行明細)

## Runtime 反註冊(`runtime remove`)

### `gogents runtime remove <workspace|name>`

- 從 config.yaml 的 `runtimes[]` 移除指定 runtime,供 am 以正式介面清除退役／測試 runtime,取代直接手改 config.yaml
- 參數接受 workspace 路徑或 runtime name;比對規則同 `runtime status`(workspace 路徑優先,以 name 指定且多筆時以最近註冊者為準)
- 命中即移除該筆並原子寫回 config.yaml(write-tmp → fsync → rename,同註冊寫入風格);其餘 runtime 條目不受影響
- **只動註冊表**:不觸碰該 runtime 的 `logs/`、workspace 內 `.gogents/`,亦不嘗試停止其 daemon(反註冊 ≠ 停機)
- 找不到對應 runtime → 明確錯誤、非零 exit

## `--json` 結構化輸出

`runtime list` 與 `runtime status` 各提供 opt-in 的 `--json` 布林旗標,對 am 轉接器提供**穩定的資料契約**,使其毋須逐行解析人類可讀顯示層(欄位增減、順序或時間格式變動不致無聲破壞解析)。`logs` 不含 `--json`(維持 JSONL 直通)。

- `runtime list --json`:stdout 輸出 runtime 陣列 JSON、exit 0;無註冊時輸出 `[]`(非文字 `(no registered runtimes)`)
- `runtime status <target> --json`:stdout 輸出單一 runtime 狀態 JSON 物件、exit 0
- 時間戳一律 RFC3339;`last_sync_at` 尚未同步時輸出空字串 `""`(不輸出零值 `0001-01-01…`)
- **結構化錯誤**:`--json` 模式失敗時 stdout 不輸出成功 payload,stderr 輸出單一錯誤物件 `{"error":{"code","message"}}`、非零 exit。`code` 以 `runtime_not_found`(target 未註冊)/ `state_not_found`(已註冊但無 state.yaml)/ `internal`(其他失敗)分類,exit code 維持現行(非零即可),不新增 exit code 方案
- **向後相容**:`--json` 為 opt-in;未帶時所有現行文字輸出與 exit 行為不變

JSON 契約的完整欄位對映與型別見 [D08 §3.4](../../designs/D08-runtime-cli.md)。

## 不變量

- config.yaml 為 runtime 註冊表(非執行必要);由 `start` upsert 寫入、`runtime remove` 移除,兩者為唯二的正式寫入路徑(am 不再手改檔案);觀測 CLI 與 am 僅以其定位 runtime,不以其選擇啟動目標
- `runtime list` / `runtime status` / `logs` 為唯讀查詢,不改變 daemon 執行行為、不寫任何檔案;`runtime remove` 僅寫 config.yaml 註冊表,不影響任何 daemon 執行

## 相關需求

- R02 - Runtime 設定檔(config.yaml 註冊表結構與寫入規則)
- R04 - Daemon 生命週期(啟動註冊步驟、state.yaml 的 `runtime_state` / `pid`)
- R09 - 日誌規範(`logs` 輸出的日誌來源)
- R11 - 本地任務源(am 為主要消費者;經此查 worker 執行狀態)
