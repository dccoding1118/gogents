# D08 — runtime-cli

涵蓋需求：R12（Runtime 註冊與觀測）、R13（Workspace 佈建的命令面）。

本模組提供 `gogents` 的 runtime 命令介面：`gogents runtime create` 由 template 佈建出一個可直接 `start` 的 workspace,template 來源預設為內建、可經 `--template` 指定（來源解析與 materialize 皆由 D09 執行）；`gogents start` 於啟動時將 runtime 註冊寫入 config.yaml（位於全域家目錄根,預設 `~/.gogents`,可經 `GOGENTS_HOME` 覆寫,見 D01 §3;以 workspace 路徑為 key）；`gogents runtime list` / `gogents runtime status` / `gogents logs` 供使用者與 agent-manager (am) 列出、查詢、匯出 runtime 狀態與日誌；`gogents runtime remove` 供 am 從註冊表反註冊退役／測試 runtime,取代手改 config.yaml。am 不維護執行狀態副本,一律經此介面查詢（單向 `am → gogents`）。

## 1. 模組架構與周邊模組關聯

runtime-cli 是**唯讀觀測層 + 註冊表寫入點 + 佈建命令面**。註冊表的實際落檔由 D01 `GlobalConfigStore` 執行,workspace 的 materialize 由 D09 `provision` 執行；本模組定義 `start` 呼叫註冊的時機（D02 §5.1 啟動序列）、三個唯讀查詢子指令、`runtime remove` 反註冊子指令,以及 `runtime create` 的旗標契約與互動確認決策。查詢一律讀既有檔案（config.yaml / state.yaml / logs），不改變 daemon 執行行為;config.yaml 的寫入僅發生於 `start` 註冊與 `runtime remove` 反註冊;`runtime create` 只寫目標 workspace,不碰 config.yaml。

**實作套件**：所有子指令（`start` / `runtime list` / `runtime status` / `logs`）集中於 `internal/cli` 命令層——`cli.Dispatch` 依 `os.Args` 分派，`cmd/gogents/main.go` 僅為薄進入點。本文件（D08）規範 R12 的註冊與觀測子指令；`start` 的生命週期行為屬 D02，`internal/cli` 內僅承載其組裝（composition root）接線。

- **依賴**
  - **D01 foundation**：`GlobalConfigStore`（Load / Upsert / Resolve / Remove，見 D01 §4）;`WorkspacePaths`（推 workspace-hash → logs 路徑、state.yaml 路徑）。
  - **D02 daemon-sync**：`StateStore.Load`（讀 `state.yaml` 的 `runtime_state` / `daemon.pid` / `current_task` / `sync`）。
  - **D04 logger**：`gogents start` 已開 logger;查詢與 `create` 子指令為短命 CLI,直接輸出 stdout,不寫 daemon 日誌。
  - **D09 provision**：`Resolve` / `Inspect` / `Plan` / `Materialize`（`runtime create` 的 template 來源解析、目標狀態判定與 materialize）。
  - **OS**：pid 存活檢核（`kill(pid, 0)` 或讀 `/proc/<pid>`）;stdin 是否為終端（`runtime create` 的確認提示）。
- **被依賴**：CLI 入口 `cmd/gogents`;am 以子行程呼叫本模組的三個查詢子指令。
- **I/O 邊界**：讀 `~/.gogents/config.yaml`、`<workspace>/.gogents/state.yaml`、`~/.gogents/logs/<hash>/*.jsonl`;寫 `~/.gogents/config.yaml`（`start` 的 upsert 與 `runtime remove` 的移除）;`runtime create` 經 D09 寫目標 workspace。（全域家目錄根預設 `~/.gogents`,可經 `GOGENTS_HOME` 覆寫。）

```mermaid
%%{init: {'theme':'dark'}}%%
graph LR
    CLI[cmd/gogents]
    D08[D08 runtime-cli]
    D01[D01 foundation<br/>GlobalConfigStore / WorkspacePaths]
    D02[D02 daemon-sync<br/>StateStore]
    D09[D09 provision<br/>Inspect / Plan / Materialize]
    OS((OS pid check))
    CFG[(~/.gogents/config.yaml)]
    ST[(state.yaml)]
    LOG[(~/.gogents/logs/&lt;hash&gt;/)]
    WS[(&lt;workspace&gt;/)]

    CLI --> D08
    D08 --> D01
    D08 --> D02
    D08 --> D09
    D08 --> OS
    D01 --> CFG
    D02 --> ST
    D08 --> LOG
    D09 --> WS
```

## 2. 行為契約

- **B1 register（由 `start` 呼叫）**：`start` 啟動時（D02 §5.1）以 `GlobalConfigStore.Upsert({name, workspace, registered_at=now})` 寫入 config.yaml,**以 `workspace` 路徑為主鍵**:已存在則覆蓋 `name` / `registered_at`,否則新增。寫入失敗僅記 warning `config.register_failed`、不中止啟動。config.yaml **不含 `active_runtime`**。
- **B2 runtime list**：`Load` config.yaml,列出所有 `runtimes`（`workspace` / `name` / `registered_at`）;不檢核 pid、不讀 state.yaml。config.yaml 不存在 → 輸出空列表 + exit 0。
- **B3 runtime status <workspace|name>**：以參數 `Resolve` 出 workspace → 讀該 workspace 的 `state.yaml` → 輸出 `runtime_state` / `pid` / `current_task` / `last_sync`。**查詢時檢核 pid**:`daemon.pid` 不存活 → 輸出 `runtime_state: down`（覆蓋 state.yaml 殘留值）。找不到註冊或 state.yaml → exit ≠ 0 + 明確訊息。
- **B4 logs <workspace|name>**：`Resolve` → 推 `~/.gogents/logs/<hash>/` → 輸出 `daemon.jsonl`（預設）;供 am `audit export` 併入。找不到 → exit ≠ 0。
- **B5 name 反查規則**：參數可為 workspace 絕對路徑或 runtime name。以 name 指定且多筆同 name（不同 workspace）→ 取 `registered_at` 最近者;以 workspace 路徑指定則精確匹配（主鍵）。remove 的目標解析同此規則。
- **B6 唯讀不變量**：`list` / `status` / `logs` **不寫任何檔案**、不改 daemon 行為、不觸碰 task.yaml / state.yaml。config.yaml 的寫入僅來自 `start` 的 register 與 `runtime remove` 的反註冊。
- **B7 runtime remove <workspace|name>**：以 B5 規則 `Resolve` 出目標,呼叫 `GlobalConfigStore.Remove` 自 config.yaml 的 `runtimes[]` 移除該筆並原子寫回;其餘條目不動。**只動註冊表**:不觸碰該 runtime 的 `logs/`、workspace 內 `.gogents/`,不停止其 daemon。找不到目標 → exit ≠ 0 + `runtime_not_found`。
- **B8 runtime create <name>**：驗證旗標（必填、列舉、per-source 相容性,§3.5）並套用預設（`--workspace`←`./<name>`、`--label`←`<name>`、`--branch`←`main`）→ `provision.Inspect` 判定目標狀態 → 依 §3.6 決定放行／確認／拒絕 → `provision.Materialize` 佈建。**只建 workspace,不註冊**:不寫 config.yaml,該 runtime 直到 `start` 才出現在 `runtime list`。**不 clone repo**:`<reponame>/` 仍由 daemon 啟動序列負責。**不刪除使用者檔案**:佈建只新增與覆蓋同名檔案,失敗時不回滾刪除,錯誤訊息列出已落地檔案（D09 B5）。本子指令不提供 `--json`。

## 3. 資料結構

### 3.1 CLI 參數

| 指令                              | 參數                | 說明                              |
| --------------------------------- | ------------------- | --------------------------------- |
| `gogents runtime list`            | `--json`（可選）    | 列出所有已註冊 runtime            |
| `gogents runtime status <target>` | `<workspace\|name>`;`--json`（可選） | 查單一 runtime 即時狀態           |
| `gogents logs <target>`           | `<workspace\|name>`;`--error`（可選,輸出 error.jsonl） | 輸出 runtime 日誌 |
| `gogents runtime remove <target>` | `<workspace\|name>`  | 從註冊表移除該 runtime            |
| `gogents runtime create <name>`   | 見 §3.5              | 佈建一個可直接 `start` 的 workspace |

`--json` 為 `runtime list` 與 `runtime status` 各自的布林旗標,位置容忍度比照現行 flag 解析（`runtime status <target> --json` 與 `runtime status --json <target>` 皆可,與 `logs --error` 一致）。`logs` 與 `runtime create` 無 `--json`（前者維持 JSONL 直通,後者非程式化查詢對象）。

### 3.2 status 輸出欄位

| 欄位            | 來源                                  | 備註                                 |
| --------------- | ------------------------------------- | ------------------------------------ |
| `workspace`     | config.yaml（Resolve）                | —                                    |
| `name`          | config.yaml                           | —                                    |
| `runtime_state`  | state.yaml `runtime_state`             | pid 不存活時覆為 `down`              |
| `pid`           | state.yaml `daemon.pid`               | —                                    |
| `alive`         | OS pid 檢核                           | bool                                 |
| `current_task`  | state.yaml `current_task.task_id`     | 無則 null                            |
| `last_sync_at`  | state.yaml `sync.last_sync_at`        | —                                    |
| `last_sync_status` | state.yaml `sync.last_sync_status` | success / failed                     |

輸出格式：預設人類可讀表列;`--json` 供 am 程式化解析（穩定資料契約,見 §3.4）。

### 3.4 `--json` 資料契約

`--json` 提供**穩定的機器可讀契約**,讓 am 轉接器不必逐行解析顯示層。未帶 `--json` 時所有文字輸出與 exit 行為完全不變（向後相容）。

**`runtime list --json`** — stdout 輸出 JSON 陣列、exit 0;無註冊時輸出 `[]`（**非**文字 `(no registered runtimes)`）。每筆對應 `RuntimeView`:

```json
[
  {"name": "coding", "workspace": "/home/u/.am/workers/coding", "registered_at": "2026-07-04T12:00:00+08:00"}
]
```

**`runtime status <target> --json`** — stdout 輸出單一 JSON 物件、exit 0,對應 `StatusView`:

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

| JSON 欄位          | 來源                       | 說明                                                             |
| ------------------ | -------------------------- | ---------------------------------------------------------------- |
| `name`             | `RuntimeView.Name`         | runtime 名                                                       |
| `workspace`        | `RuntimeView.Workspace`    | workspace 路徑（註冊主鍵）                                       |
| `registered_at`    | `RuntimeView.RegisteredAt` | 註冊時間,RFC3339                                                 |
| `runtime_state`    | `StatusView.RuntimeState`  | `idle/executing/verifying/reporting/failed/broken`;pid 不存活為 `down` |
| `pid`              | `StatusView.Pid`           | 記錄的 daemon PID                                                |
| `alive`            | `StatusView.Alive`         | 查詢時 pid 檢核結果                                              |
| `current_task`     | `StatusView.CurrentTask`   | 當前任務 id,空字串＝無                                           |
| `last_sync_at`     | `StatusView.LastSyncAt`    | RFC3339;尚未同步時輸出空字串 `""`（不輸出 `0001-01-01…`）        |
| `last_sync_status` | `StatusView.LastSyncStatus`| 最近同步結果                                                     |

時間戳一律 RFC3339。`LastSyncAt` 為 `time.Time`,零值 marshal 時須輸出空字串（見 §4 的自訂 JSON 型別）。

**`--json` 錯誤契約** — 失敗時 stdout 不輸出成功 payload;stderr 輸出單一 JSON 錯誤物件、非零 exit:

```json
{"error": {"code": "runtime_not_found", "message": "runtime \"coding\" not registered"}}
```

| `code`              | 情境                                                        |
| ------------------- | --------------------------------------------------------- |
| `runtime_not_found` | `Resolve` 失敗:target 不在 `~/.gogents/config.yaml` 註冊表 |
| `state_not_found`   | 已註冊但無 `state.yaml`（尚未 `start` 過）                 |
| `internal`          | 其他失敗（load 錯誤等）                                    |

exit code 維持現行（非零即可）,失敗分類以 `error.code` 表達,不新增 exit code 方案。非 `--json` 模式的錯誤輸出（stderr `error: …` 文字）維持不變。

### 3.3 pid 存活檢核

以 `syscall.Kill(pid, 0)`（或讀 `/proc/<pid>`）判定;回 `ESRCH` → 不存活。存活判定**不驗證該 pid 確為 gogents daemon**（目前版本簡化;pid 重用屬罕見,由 `daemon.started_at` 佐證留待後續）。

### 3.5 `runtime create` 旗標

| 旗標 | 必填 | 預設 | 落點 |
| --- | --- | --- | --- |
| `<name>`（位置參數） | 是 | — | `Spec.Name` → `runtime_name` |
| `--template` | 否 | 空（＝內建 template） | `provision.Resolve` 的來源選擇（§3.7） |
| `--workspace` | 否 | `./<name>` | 佈建目標路徑 |
| `--repo` | 是 | — | `Spec.RepoURL` |
| `--branch` | 否 | `main` | `Spec.Branch` |
| `--agent` | 是 | — | `Spec.Agent`;列舉:`claude` |
| `--model` | 是 | — | `Spec.Model` |
| `--source` | 是 | — | `Spec.Source`;列舉:`local` / `github_issue` |
| `--label` | 否 | `<name>` | `Spec.Label` |
| `--instance` | `--source=github_issue` 時必填 | — | `Spec.Instance` |
| `--tickets-dir` | 否 | 空 | `Spec.TicketsPath` |
| `--force` | 否 | false | 跳過非空目錄確認（§3.6） |

**per-source 相容性**：`--instance` 僅 `github_issue` 可用、且為其必填;`--tickets-dir` 僅 `local` 可用。旗標與 `--source` 不相容時報錯,不靜默忽略。

`name` 為位置參數,與 `runtime status` / `runtime remove` 的 target 形式一致。

### 3.6 非空目錄確認

| `provision.Inspect` 結果 | `--force` | stdin 為終端 | 行為 |
| --- | --- | --- | --- |
| `TargetAbsent` / `TargetEmptyDir` | — | — | 直接佈建 |
| `TargetNotDir` | 任意 | — | 拒絕、exit ≠ 0（`--force` 不繞過） |
| `TargetNonEmptyDir` | true | — | 直接佈建 |
| `TargetNonEmptyDir` | false | 否 | 拒絕、exit ≠ 0,訊息提示改用 `--force` |
| `TargetNonEmptyDir` | false | 是 | 以 `provision.Plan` 列出將寫入／覆蓋的路徑 → 提示 → 讀 stdin;`y` / `yes` 才佈建,否則中止、exit ≠ 0、不寫入 |

提示訊息需明確表達後果：目錄非空,將寫入 agent 運作環境配置（列出目標路徑,標示哪些為既有檔案的覆蓋）,其餘檔案不受影響。**非互動時不預設同意**:CI／腳本情境下靜默覆蓋使用者目錄的風險高於失敗。

**終端判定**：以真正的 TTY 檢核（`term.IsTerminal`）判斷 stdin,非以 `os.ModeCharDevice` 判斷——`/dev/null` 亦為 character device,後者會把 `< /dev/null` 誤判為互動終端。

### 3.7 `--template` 的命令層職責

來源解析規則屬 D09（`provision.Resolve`,見 D09 §3.1）;本層只負責兩件事:

- **空值攔截**:`--template=`（給了旗標但值為空）視為旗標驗證錯誤,exit ≠ 0。空字串在 D09 是「未給旗標 → 內建 template」的訊號,若原樣傳下去會把一個明顯的輸入錯誤靜默解讀成預設行為。省略旗標才是預設。
- **解析時機**:`Resolve` 於 `Inspect` 之前呼叫,使來源解析失敗一律發生在任何寫入與任何確認提示之前——不對一個註定失敗的佈建提示使用者確認覆蓋。

`--template` **不參與 per-source 相容性驗證**:template 來源與 `--source` 正交,任何 template 樹皆可搭配任何 source。

## 4. 關鍵介面

```go
package cli

// Register 由 gogents start 於啟動序列呼叫（D02 §5.1）。
func Register(gcfg foundation.GlobalConfigStore, name, workspace string) error

// List 對應 gogents runtime list。
func List(gcfg foundation.GlobalConfigStore) ([]RuntimeView, error)

// Status 對應 gogents runtime status;內含 pid 檢核與 down 覆寫。
func Status(gcfg foundation.GlobalConfigStore, target string) (StatusView, error)

// Logs 對應 gogents logs;回傳日誌檔路徑供 CLI 串流輸出。
func Logs(gcfg foundation.GlobalConfigStore, paths foundation.WorkspacePaths, target string, errOnly bool) (logPath string, err error)

// Remove 對應 gogents runtime remove;以 B5 規則解析 target 後自註冊表移除。
func Remove(gcfg foundation.GlobalConfigStore, target string) error

// Create 對應 gogents runtime create（B8）。confirm 在目標為非空目錄且未帶
// --force 時被呼叫,由命令層注入（互動終端讀 stdin;非互動終端一律回 false）;
// 回傳 written 為已落地的 workspace 相對路徑,失敗時一併回傳（D09 B5）。
func Create(opts CreateOpts, confirm func(entries []provision.Entry) bool) (written []string, err error)

// CreateOpts 為 §3.5 旗標解析後的結果;Validate 檢查必填、列舉與 per-source
// 相容性,並套用預設值（Workspace←./<Name>、Label←Name、Branch←"main"）。
type CreateOpts struct {
    Name        string
    Template    string
    Workspace   string
    RepoURL     string
    Branch      string
    Agent       string
    Model       string
    Source      string
    Label       string
    Instance    string
    TicketsPath string
    Force       bool
}

func (o *CreateOpts) Validate() error

var ErrWorkspaceNotEmpty = errors.New("workspace is not empty")

type RuntimeView struct {
    Name         string `json:"name"`
    Workspace    string `json:"workspace"`
    RegisteredAt string `json:"registered_at"` // RFC3339 字串
}

type StatusView struct {
    RuntimeView
    RuntimeState   string   `json:"runtime_state"`   // state.yaml 值;pid 不存活時為 "down"
    Pid            int      `json:"pid"`
    Alive          bool     `json:"alive"`
    CurrentTask    string   `json:"current_task"`    // 空字串表示無
    LastSyncAt     time.Time `json:"last_sync_at"`   // 零值 marshal 為 ""（見下）
    LastSyncStatus string   `json:"last_sync_status"`
}
```

`Status` 內部：`gcfg.Resolve(target)` → 建 `WorkspacePaths(workspace)` → `StateStore(paths).Load()` → pid 檢核 → 若 `!alive` 則 `RuntimeState = "down"`。

**JSON 輸出型別**：`--json` 直接對 `RuntimeView` / `StatusView` 加 JSON tag。`LastSyncAt` 為 `time.Time`,其零值以標準 marshal 會輸出 `0001-01-01T…`;因此 status 的 JSON 輸出改用一個 DTO（或以 `MarshalJSON` 自訂），將零值 `LastSyncAt` 輸出為空字串 `""`,非零值輸出 RFC3339。其餘欄位直通。

**`--json` 錯誤物件**：CLI 層將 `Resolve` / `Status` 回傳的 error 對映為 `{"error":{"code","message"}}`（§3.4）——`ErrRuntimeNotRegistered` → `runtime_not_found`;state.yaml 缺失 → `state_not_found`;其餘 → `internal`——序列化至 stderr、非零 exit。

## 5. 程式資料流

### 5.1 register（start 啟動序列內）

```
start:
  ...（preflight、logger、repo）
  gcfg := D01.NewGlobalConfigStore()
  err := cli.Register(gcfg, cfg.RuntimeName, ws)
     └─ gcfg.Upsert({name, workspace=ws, registered_at=now})  // 以 workspace 為 key
  if err != nil: emit warning "config.register_failed"          // 不中止
  else:          emit info "daemon.registered" {workspace, name}
  ...（state.yaml、重啟恢復、主迴圈）
```

### 5.2 runtime status

```
Status(gcfg, target):
  reg, err := gcfg.Resolve(target)          // workspace 路徑或 name
  if err == ErrRuntimeNotRegistered: return {}, err
  paths := NewWorkspacePaths(reg.Workspace)
  st, err := StateStore(paths).Load()
  if err != nil: return {}, err             // state.yaml 不存在 / 損毀
  alive := pidAlive(st.Daemon.Pid)
  view := StatusView{ ...from reg & st...,  Alive: alive }
  if !alive: view.RuntimeState = "down"      // 覆蓋殘留值（死掉的 daemon 無法自我更新）
  return view, nil
```

### 5.3 logs

```
Logs(gcfg, _, target, errOnly):
  reg := gcfg.Resolve(target)
  paths := NewWorkspacePaths(reg.Workspace)
  return errOnly ? paths.ErrorLog() : paths.DaemonLog(), nil
  // CLI 層負責 open + 串流至 stdout（tail / cat 語義由實作決定）
```

### 5.4 runtime remove

```
Remove(gcfg, target):
  return gcfg.Remove(target)         // 內部以 Resolve 規則定位 → 移除該筆 → 原子寫回
  // 命中即移除;未命中回 ErrRuntimeNotRegistered（CLI 層對映 exit ≠ 0 / runtime_not_found）
  // 只動 config.yaml 註冊表,不觸碰 logs/ 或 workspace 內 .gogents/
```

### 5.5 runtime create

```
Create(opts, confirm):
  opts.Validate()                            // 必填 / 列舉 / per-source 相容 / 套預設
  src := provision.Resolve(opts.Template)    // 失敗 → return nil, ErrTemplateNotFound
                                             // 先於 Inspect:解析失敗不提示、不寫入（§3.7）
  state := provision.Inspect(opts.Workspace)
  switch state:
    TargetNotDir      → return nil, ErrWorkspaceExists
    TargetNonEmptyDir → if !opts.Force:
                            entries := provision.Plan(src, opts.Workspace)
                            if !confirm(entries): return nil, ErrWorkspaceNotEmpty
    _                 → 放行
  spec := Spec{Name, RepoURL, Branch, Agent, Model, Source, Label, Instance, TicketsPath}
  return provision.Materialize(src, opts.Workspace, spec)
  // 不寫 config.yaml（註冊留給 start）;不 clone repo（留給 daemon 啟動序列）

// 命令層的 confirm 注入:
//   非互動終端     → func(...) bool { return false }   // 由訊息提示 --force
//   互動終端       → 列出 entries（標示 Overwrite）→ 讀 stdin → y/yes 為 true
```

## 6. 錯誤處理

| 情境                                  | 分類     | 處理                                                       |
| ------------------------------------- | -------- | ---------------------------------------------------------- |
| register upsert 失敗（磁碟 / 權限）   | 暫時     | warning `config.register_failed`;**不中止 start**          |
| status / logs 目標未註冊              | 使用者錯 | exit ≠ 0;訊息列出可用 runtime 提示 `runtime list`          |
| status 目標 state.yaml 不存在 / 損毀  | 使用者錯 | exit ≠ 0;訊息指出 workspace 可能未曾啟動或檔案損毀         |
| status pid 不存活                     | 正常     | `runtime_state: down`;exit 0（查詢本身成功）                |
| name 多筆同名                         | 歧義     | 取 `registered_at` 最近者;（可選）warning 提示以 workspace 精確指定 |
| logs 日誌檔不存在                     | 使用者錯 | exit ≠ 0;訊息指出該 runtime 尚無日誌                       |
| remove 目標未註冊                     | 使用者錯 | exit ≠ 0 + `runtime_not_found`;不改動 config.yaml          |
| remove 寫回 config.yaml 失敗（磁碟 / 權限） | 暫時 | exit ≠ 0 + `internal`;註冊表維持原狀（原子寫入,無半寫）    |
| create 旗標缺漏 / 不在列舉 / 與 `--source` 不相容 | 使用者錯 | usage 或明確訊息;exit ≠ 0;不寫入                    |
| create `--template` 解析出的目錄不存在 / 非目錄 | 使用者錯 | `ErrTemplateNotFound`;exit ≠ 0;不寫入、不提示確認;訊息含查找的絕對路徑與其來源 |
| create `--template` 值為空字串               | 使用者錯 | 旗標驗證錯誤;exit ≠ 0;不寫入（不等同省略,§3.7）           |
| create 目標存在但非目錄                | 使用者錯 | `ErrWorkspaceExists`;exit ≠ 0;不寫入（`--force` 不繞過）   |
| create 目標非空、非互動終端且無 `--force` | 使用者錯 | `ErrWorkspaceNotEmpty`;exit ≠ 0;不寫入;訊息提示 `--force` |
| create 確認被拒                        | 正常     | exit ≠ 0;不寫入（使用者主動中止,非錯誤語意）               |
| create materialize 失敗（template / 磁碟 / 權限） | 暫時 / 程式錯誤 | exit ≠ 0;錯誤訊息列出已落地檔案;**不回滾刪除**（D09 B5） |

查詢子指令為短命 CLI,錯誤直接寫 stderr + 非零 exit,不寫 daemon.jsonl。以上為預設文字模式;`--json` 模式下同樣的失敗改以結構化錯誤物件（`error.code`,見 §3.4）輸出至 stderr,exit code 與文字模式相同。

## 7. 驗收方式

本模組的 `start` 註冊部分由 **D02 啟動序列驗收**覆蓋（觀察 `~/.gogents/config.yaml` 出現對應 runtime 條目）;三個查詢子指令屬**可執行產出**,以真實命令驗收。

### 7.1 前置

- 至少一個已 `gogents start` 過的 workspace（config.yaml 有註冊、state.yaml 存在）。

### 7.2 正常路徑

| 步驟 | 動作                                                   | 觀察點                                                                          |
| ---- | ------------------------------------------------------ | ------------------------------------------------------------------------------- |
| A    | `gogents start --workspace $WS &`                      | `~/.gogents/config.yaml` 出現 `{name, workspace=$WS, registered_at}`;`daemon.registered` 事件 |
| B    | 於同路徑刪 workspace 後重建、再 `start`                | config.yaml 該 workspace 條目被 **覆蓋更新**（非新增第二筆）                    |
| C    | `gogents runtime list`                                 | 列出 $WS 對應 runtime                                                           |
| D    | `gogents runtime status $WS`                           | 輸出 `runtime_state`（idle/executing…）、`pid`、`current_task`、`last_sync`      |
| E    | `kill -9 <pid>` 後 `gogents runtime status $WS`        | `runtime_state: down`、`alive: false`;exit 0                                    |
| F    | `gogents logs $WS`                                      | 輸出該 runtime `daemon.jsonl` 內容;`--error` 輸出 `error.jsonl`                 |
| G    | `gogents runtime list --json`                           | stdout 輸出 JSON 陣列;無註冊時輸出 `[]`、exit 0                                 |
| H    | `gogents runtime status $WS --json`                     | stdout 輸出單一 JSON 物件（欄位如 §3.4）;`last_sync_at` 未同步時為 `""`          |
| I    | `gogents runtime remove $WS` 後 `gogents runtime list`  | config.yaml `runtimes[]` 不再含 $WS 條目、其餘不動;`logs/` 與 workspace 內 `.gogents/` 皆保留 |
| J    | `gogents runtime create demo --repo <url> --agent claude --model <m> --source local` | 於 `./demo` 產出 `.gogents/runtime.yaml`（`runtime_name: demo`、`filter.label: demo`、無 `source.path`／`source.instance`）、`.claude/CLAUDE.md`、`.claude/skills/`、`.mcp.json`;config.yaml **不變**（未註冊） |
| K    | 對 J 產出的 workspace 直接 `gogents start --workspace ./demo` | 通過 config schema 驗證並啟動;config.yaml 此時才出現該 runtime |
| L    | `runtime create` 指向既有非空目錄（互動終端） | 列出將寫入／覆蓋的路徑並提示;答 `n` → 不寫入、exit ≠ 0;答 `y` → 佈建,目錄內其餘既有檔案不變 |
| M    | 同 L 但 stdin 非終端（如 `< /dev/null`）      | 不提示、exit ≠ 0、不寫入,訊息提示 `--force`;改帶 `--force` → 直接佈建 |
| N    | 複製 `templates/base/` 到 `./my-tpl`、刪其 `.mcp.json`,`runtime create demo2 --template ./my-tpl …` | 產出僅含 `./my-tpl` 的內容;`./demo2/.mcp.json` **不存在**（不與內建 template 疊加） |
| O    | `GOGENTS_TEMPLATES=$PWD/tpls gogents runtime create demo3 --template svc …` | 讀 `$PWD/tpls/svc/` 佈建;同名稱在未設該變數時改讀 `./svc/` |

### 7.3 錯誤路徑

| 破壞                              | 預期                                                       |
| --------------------------------- | ---------------------------------------------------------- |
| `runtime status <未註冊 name>`    | exit ≠ 0;提示 `runtime list`                              |
| `runtime status` 指向 state.yaml 缺失的 workspace | exit ≠ 0;訊息指出未曾啟動                  |
| `logs <未註冊>`                   | exit ≠ 0                                                   |
| `runtime status <未註冊> --json`  | stderr 輸出 `{"error":{"code":"runtime_not_found",…}}`、exit ≠ 0;stdout 無 payload |
| `runtime status --json` 指向 state.yaml 缺失的 workspace | stderr `error.code = state_not_found`、exit ≠ 0 |
| `runtime remove <未註冊>`         | exit ≠ 0;config.yaml 不被改動                              |
| `runtime create` 缺 `--repo` / `--source github_issue` 缺 `--instance` | exit ≠ 0;不建立任何目錄                |
| `runtime create --source local --instance x`（旗標與 source 不相容） | exit ≠ 0 + 明確訊息;不寫入            |
| `runtime create` 目標為既有一般檔案 | exit ≠ 0;該檔案不被改動（`--force` 亦然）                |
| `runtime create --template ./nope`（目錄不存在） | exit ≠ 0 + 訊息含查找路徑;**不落回內建 template**、不建立目標目錄 |
| `runtime create --template svc` 且 `GOGENTS_TEMPLATES` 設定但其下無 `svc/` | exit ≠ 0;不改查當前目錄的 `./svc/`、不落回內建 template |

### 7.4 通過條件

- `start` 後 config.yaml 以 workspace 為 key upsert（同路徑重跑不長出重複條目、無 `active_runtime` 欄位）。
- `runtime status` 於 daemon 存活時回報 state.yaml 的 `runtime_state`,daemon 不存活時回報 `down`。
- 三個查詢子指令全程**不修改** config.yaml / state.yaml / task.yaml（可用 mtime 觀察）。
- `runtime remove` 僅從 config.yaml 移除目標條目,不觸碰 state.yaml / task.yaml / logs;未註冊目標回非零 exit 且 config.yaml 不變。
- `runtime create` 以內建 template 產出的 workspace 可直接 `start`;`create` 全程不寫 config.yaml、不 clone repo、不刪除目標目錄下任何既有檔案（含失敗路徑）。
- `--template` 選出唯一來源樹:落地檔案集恰為該樹內容,不與內建 template 疊加;解析失敗一律非零 exit 且不寫入,不靜默落回內建 template。
