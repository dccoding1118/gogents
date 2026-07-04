# R02 - Runtime 設定檔

> 主需求檔：[requirement.md](../requirement.md)

## 目的

定義 workspace 層級設定檔(runtime.yaml)與全域設定檔(config.yaml)的格式、欄位與驗證規則,以及 daemon 啟動時的前置檢查。

## runtime.yaml

位置:`<workspace-root>/.gogents/runtime.yaml`

```yaml
runtime_name: "my-project"        # runtime 識別名稱

project:
  repo:
    url: "https://github.com/owner/repo.git"
    default_branch: "main"

source:
  type: "github_issue"            # 支援 github_issue 或 local
  instance: "owner/repo"
  filter:
    label: "gogents"
  sync_interval: "2m"

agent:
  type: "claude"                  # 目前支援 claude
  model: "claude-3-5-sonnet-20241022"

task_policy:
  max_failures: 3                 # 任務連續失敗門檻

assignee: "windysky912"           # 使用者 ID，具體語義由 source.type 決定 (如 github username)
```

## source 區塊

採用「type + 共通屬性」的抽象設計,syncer 只認抽象 `TaskSource`,依 `source.type` 對應不同來源實作。

- **抽象屬性**: `type`、`filter.label`、`sync_interval` 為各型別共用;`instance` / `path` 為型別專屬
- **Type 對應實作**: 每個 type 由獨立 source adapter 實作(TaskSource / SourceProbe / ContentFetcher / FeedbackReporter)
- **目前支援範圍**: `github_issue` 與 `local`

### `github_issue`

```yaml
source:
  type: "github_issue"
  instance: "owner/repo"          # 目標 repo
  filter:
    label: "gogents"              # 觸發 label
  sync_interval: "2m"
```

任務單位為 GitHub Issue;細節見 R05、R08。

### `local`

對接 agent-manager (am):讀本地統一 ticket inbox,認領自身 label 且 `status == queued` 的 ticket。

```yaml
source:
  type: "local"
  path: "~/.am/tickets"           # am 的統一 ticket inbox
  filter:
    label: "<this-worker-label>"  # 僅認領此 label 且 status=queued 的 ticket
  sync_interval: "30s"
```

- `path`: ticket inbox 目錄;省略時預設 `~/.am/tickets`
- `filter.label`: 本 worker 專長 label;多 daemon 共讀同一 inbox、各取各 label、無爭用
- `instance` 不使用

ticket 檔為 YAML,欄位由 am 定義;gogents 僅讀執行所需欄位並回寫終態。任務單位與驗證、回報細節見 R11。

## agent 區塊設計原則

採用「type + 共通屬性」的抽象設計,executor 模組依 `type` 對應不同 coding agent 的呼叫方式與指令參數。

- **抽象屬性**: 目前僅定義 `type` 與 `model`
- **Type 對應實作**: 每個 type 由獨立 executor 實作
- **目前支援範圍**: 實作 `type: claude`
- **自訂參數**: 目前不提供 `args` 逃生艙口

### Claude executor 行為

`type: claude` 的 executor 將屬性翻譯為非互動模式下傳入 input.md (具體 flag 依 Claude Code CLI 版本而定)。

執行細節(工作目錄、stdout 重導等)定義於 R06。

## task_policy 區塊

| 欄位           | 預設值 | 說明                                        |
| -------------- | ------ | ------------------------------------------- |
| `max_failures` | 3      | 任務連續失敗達此次數後,不再自動重跑(見 R03) |

## config.yaml

位置:`~/.gogents/config.yaml`
職責:全域 runtime 註冊表,供觀測 CLI(`gogents runtime list/status`、`gogents logs`,見 R12)與 am 定位各 worker。**由 `gogents start` 自動維護**,非人工撰寫。

```yaml
runtimes:
  - name: "my-project"
    workspace: "/Users/me/projects/my-workspace"
    registered_at: "2026-04-07T10:00:00Z"
```

### 註冊寫入規則(由 `start` 執行)

- daemon 啟動時(如同開啟 logs)將本 runtime 註冊寫入 config.yaml
- **以 `workspace` 路徑為主鍵(upsert-by-workspace)**:workspace 目錄可能被檔案系統刪除、之後於同路徑重建新 runtime;以路徑為 key 才能覆蓋舊註冊
- 若該 workspace 路徑已存在於註冊表 → **覆蓋更新**其 `name` 與 `registered_at`;否則新增一筆
- **不含 `active_runtime` 欄位**:daemon 啟動由 `--workspace` 參數明確指定,不從 config.yaml 選擇

### RuntimeRegistration 欄位

| 欄位            | 型別    | 說明                          |
| --------------- | ------- | ----------------------------- |
| `name`          | string  | `runtime.yaml` 的 runtime_name |
| `workspace`     | string  | workspace 根目錄絕對路徑(主鍵) |
| `registered_at` | RFC3339 | 最近一次 `start` 註冊時間     |

## runtime_name 規則

- 供人辨識與觀測 CLI 顯示;與 workspace 路徑解耦
- 建議使用 kebab-case
- 註冊表以 workspace 路徑為主鍵,`name` 非唯一鍵(同 name 可對應不同已刪除又重建的 workspace,以路徑區分)

## 啟動前置檢查

Daemon 啟動時依序執行以下檢查,任一失敗即拒絕啟動並寫入 stderr 與 error.jsonl。

### 階段 1:設定檔驗證

- 必填欄位齊全(runtime_name、project.repo.url、source.*、agent.type、agent.model)
- `source.type` 為支援的值(`github_issue` 或 `local`)
- 依 `source.type` 檢查其專屬必填欄位:`github_issue` 需 `source.instance`;`local` 需 `source.filter.label`(`source.path` 省略時預設 `~/.am/tickets`)
- `agent.type` 為支援的值(`claude`)
- `sync_interval` 可被 `time.ParseDuration` 解析
- `task_policy.max_failures` 為正整數(未設定則用預設值 3)

### 階段 2:任務來源工具鏈檢查

依 `source.type` 執行對應的前置檢查,驗證任務來源可以被 daemon 實際存取。每個 source type 都由對應的 source adapter 模組實作自己的檢查邏輯。

**`source.type: github_issue` 的檢查項目:**

1. **工具安裝**: 確認 `gh` 指令在 PATH 中可執行
2. **授權狀態**: 執行 `gh auth status`, 確認已登入且 token 未過期
3. **網路連線與權限**: 以輕量指令(例如 `gh api user` 或 `gh repo view <instance>`)驗證可連線到 GitHub 且對目標 repo 有存取權
4. **目標 repo 存在**: 確認 `source.instance` 指定的 repo 實際存在且當前使用者可讀

**`source.type: local` 的檢查項目:**

1. **inbox 可讀**: 確認 `source.path`(預設 `~/.am/tickets`)存在且可讀取;不存在或無讀權限即拒啟

檢查失敗時,error.jsonl 記錄具體失敗原因(例如「`gh` 未安裝」「`gh auth status` 顯示未登入」「repo `owner/repo` 不存在或無權限」「ticket inbox `~/.am/tickets` 不存在或無法讀取」),讓使用者可直接修正。

### 階段 3:agent 工具鏈檢查

依 `agent.type` 執行對應檢查,由各 agent executor 模組實作。

**`agent.type: claude` 的檢查項目:**

1. **工具安裝**: 確認 `claude` 指令在 PATH 中可執行
2. **授權狀態**: 目前不主動驗證(claude code CLI 的授權機制由使用者預先完成),若實際執行時才發現未授權,視為任務執行失敗(見 R07)

### 階段 4:Git 工具鏈檢查

- `git` 指令在 PATH 中可執行
- 可存取 `project.repo.url`(例如 `git ls-remote <url>` 成功)

## 設定變更行為

目前不支援熱重載。runtime.yaml 或 config.yaml 變更後需重啟 daemon。

## 未來擴充預留欄位

```yaml
retention:                        # 預留:任務完成後的保留策略
  done: "30d"
  failed: "90d"
```

## 相關需求

- R01 - Workspace 結構
- R03 - 任務狀態模型(max_failures 互動)
- R04 - Daemon 生命週期(啟動序列呼叫本文件定義的前置檢查)
- R05 - GitHub Issue 同步(使用 source 設定、共用啟動檢查結果)
- R06 - 任務執行流程(使用 agent 設定)
- R09 - 日誌規範(error 寫入規範)
- R11 - 本地任務源(`source.type: local` 設定與前置檢查)
- R12 - Runtime 註冊與觀測(config.yaml 註冊表由 `start` 維護)
