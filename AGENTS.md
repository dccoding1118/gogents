# GoGents 專案開發指南 (AGENTS.md)

這是一份為開發者與 AI Agent 準備的專案指引文件，旨在協助快速了解專案結構、任務執行流程以及開發環境配置。

## 1. 專案概覽 (Project Overview)
`gogents` 是一個基於 Go 語言開發的自動化系統，其核心工作流為：
1. **Sync**: 從外部 Task Source 同步指派任務。來源型別由 `source.type` 決定：`github_issue` (GitHub Issue) 或 `local` (本地 `~/.am/tickets/` ticket inbox，對接 agent-manager)。
2. **Execute**: 根據任務定義，呼叫 LLM Agent (Claude Code) 在本地工作區執行任務 (agent 工作目錄為 workspace 根，使 workspace 級 `.claude/`、`.mcp.json` 生效)。
3. **Verify & Report**: 執行完成後，依 ticket 宣告的 `verify` (或 fallback `result.json`) 判定成敗，將結果與日誌回傳至來源 (GitHub 留言 / 回寫 ticket 檔)。

workspace 由 `gogents runtime create <name>` 依內建 template 佈建（`.gogents/runtime.yaml` 與 `.claude/`、`.mcp.json` 等 agent runtime 元素），產出後即可 `gogents start`。

作為 agent-manager (am) 的執行面：am 派工到 `~/.am/tickets/`，gogents worker daemon 依 label 認領執行、回寫終態；am 經 `gogents runtime status` / `gogents logs` 查詢執行狀態 (單向 `am → gogents`)。

## 2. 目錄結構與導覽 (Directory Structure)

### 核心入口
- `cmd/gogents/`: 應用程式進入點 (main.go)，薄進入點，呼叫 `internal/cli` 的 `Dispatch`。
- `internal/cli/`: 命令層 (`start`、`runtime create`、`runtime list`、`runtime status`、`runtime remove`、`logs`)；`start` 的組裝亦在此。
- `templates/base/`: 內建 workspace template，以 `go:embed` 打包進 binary；repo 內保留原始檔供複製修改。
- `bin/`: 編譯後的執行檔輸出目錄。
- `logs/`: 測試報告、覆蓋率與執行日誌輸出目錄。

### 內部模組 (`internal/`)
- `internal/foundation/`: 底層基礎建設。
    - `config.go`: 全域配置載入；`<全域家目錄根>/config.yaml` runtime 註冊表 (以 workspace 路徑為 key 的 upsert；`runtime remove` 反註冊移除)。
    - `workspace.go`: 工作區路徑管理與目錄結構定義；全域家目錄根解析 (`GOGENTS_HOME` 覆寫，預設 `~/.gogents`)，供 config 與 logs 共用。
    - `task_store.go`: 任務狀態持久化管理。
- `internal/daemon/`: 守護進程與同步邏輯。
    - `daemon.go`: 主程序生命週期控制。
    - `syncer.go`: 處理定時從外部 Source 同步任務的邏輯。
- `internal/agent/`: LLM Agent 適配器。
    - `dispatcher.go`: 依 `agent.type` 選出對應的 Agent 實作。
    - `claude.go`: 具體的 Agent 實現。
- `internal/source/`: 外部任務來源適配器 (TaskSource / SourceProbe / ContentFetcher / FeedbackReporter)。
    - `github.go`: `github_issue` 實作，透過 `gh` CLI 存取 Issues。
    - `local*.go`: `local` 實作，讀 `~/.am/tickets/` ticket inbox、依 label 認領、回寫終態。
    - `dispatcher.go`: 依 `source.type` 分派 concrete strategy 的 factory (`NewTaskSourceFromConfig` 等)。
- `internal/execution/`: 任務執行引擎。
    - `runner.go`: 控制任務執行的具體流程 (Prepare -> Run -> Verify -> Judge)；agent 工作目錄為 workspace 根。
    - `judge.go`: 判別執行結果是否成功 (ticket `verify` 指令 exit code，或 fallback `result.json`)。
- `internal/provision/`: workspace 佈建 (`runtime create` 的 materialize)。
    - 走訪 template 樹：`.tmpl` 經 `text/template` 展開並去後綴，其餘原樣複製；來源相對路徑即 workspace 相對路徑。
    - `Inspect` / `Plan` 供命令層判定目標狀態與列出將寫入／覆蓋的檔案；互動確認留在 `internal/cli`。
    - **不刪除任何檔案**（含失敗路徑）；失敗時回傳已落地檔案清單供呼叫端列出。
- `internal/gitops/`: Git 操作封裝。
- `internal/logger/`: 結構化日誌記錄工具。

### 文檔資源 (`docs/`)
- `docs/designs/`: 詳細架構設計文件 (D01-D09)。開發前務必閱讀對應模組的設計。
- `docs/requirements/`: 需求規格文件 (R01-R13)。
- `docs/changes/`: 變更提案 (CH-00x)，描述跨模組的功能變更範圍。

## 3. 環境設定與指令 (Environment & Mise)

本專案使用 [mise](https://mise.jdx.st/) 管理工具鏈 (Go, Linter) 與常見開發任務。

### mise Tasks
以下為專案常用指令 (可透過 `mise run <task>` 執行)：

| 指令    | 功用                              | 核心路徑       |
| :------ | :-------------------------------- | :------------- |
| `build` | 編譯產出 `bin/gogents`            | `cmd/gogents/` |
| `test`  | 執行所有單元測試                  | `./...`        |
| `lint`  | 執行 `golangci-lint` 檢查代碼品質 | -              |
| `fmt`   | 使用 `gofumpt` 強制格式化代碼     | -              |

## 4. Agent 開發快速定位 (TL;DR)

如果你是一個正在此專案中工作的 Agent，請參考以下定位點：

- **我要新增一個 CLI 指令**: 修改 `internal/cli/cli.go` 的 `Dispatch`（`cmd/gogents/main.go` 只是薄進入點）。
- **我要調整 workspace 佈建或 template 內容**: template 檔案改 `templates/base/`（新增檔案無需改程式）；materialize 規則見 `internal/provision/`；旗標與確認流程見 `internal/cli/`。
- **我要調整配置參數**: 修改 `internal/foundation/config.go`。
- **我要修改 Agent 的 Prompt 或行為**: 修改 `internal/agent/` 下對應的檔案。
- **我要改變任務狀態的處理方式**: 修改 `internal/foundation/task_store.go`。
- **我要解決同步 (Sync) 失敗的問題**: 查看 `internal/source/` 下的對應適配器與 `internal/daemon/syncer.go`。
- **任務執行邏輯有誤 (Execution)**: 查看 `internal/execution/runner.go`。
- **我要新增一種任務來源 (如 local)**: 於 `internal/source/` 新增 concrete strategy，並在 `internal/source/dispatcher.go` 的各 factory `switch` 加 case。
- **我要調整驗證方式 (verify / result.json)**: 修改 `internal/execution/judge.go`。
- **我要調整 runtime 註冊或觀測 CLI**: 註冊寫入見 `internal/foundation/config.go` 與 daemon `start`；查詢指令見 `cmd/gogents/`；`runtime_state` 見 `internal/daemon/state_store.go`。

## 5. 測試與品質規範 (Testing & Quality)
- **單元測試**: 所有核心邏輯必須撰寫單元測試，且 **涵蓋率 (Coverage) 至少需達到 80%**。
- **報告產出**: 單元測試結果應輸出至 `logs/unit_test_report.log`，不得直接輸出於根目錄。
- **Linting**: 提交代碼前務必通過 `mise run lint`。

---
*註：本文件應隨著專案演進持續更新。*
