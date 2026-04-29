# GoGents 專案開發指南 (AGENTS.md)

這是一份為開發者與 AI Agent 準備的專案指引文件，旨在協助快速了解專案結構、任務執行流程以及開發環境配置。

## 1. 專案概覽 (Project Overview)
`gogents` 是一個基於 Go 語言開發的自動化系統，其核心工作流為：
1. **Sync**: 從外部 Tasks Source (目前為 GitHub) 同步指派任務。
2. **Execute**: 根據任務定義，呼叫 LLM Agent (Gemini/Claude) 在本地工作區執行任務。
3. **Report**: 執行完成後，將結果與日誌回傳至來源平台。

## 2. 目錄結構與導覽 (Directory Structure)

### 核心入口
- `cmd/gogents/`: 應用程式進入點 (main.go)，處理 CLI 指令定義。
- `bin/`: 編譯後的執行檔輸出目錄。
- `logs/`: 測試報告、覆蓋率與執行日誌輸出目錄。

### 內部模組 (`internal/`)
- `internal/foundation/`: 底層基礎建設。
    - `config.go`: 全域配置與環境變數載入。
    - `workspace.go`: 工作區路徑管理與目錄結構定義。
    - `task_store.go`: 任務狀態持久化管理。
- `internal/daemon/`: 守護進程與同步邏輯。
    - `daemon.go`: 主程序生命週期控制。
    - `syncer.go`: 處理定時從外部 Source 同步任務的邏輯。
- `internal/agent/`: LLM Agent 適配器。
    - `dispatcher.go`: 根據配置挑選適當的 Agent (如 Gemini 或 Claude)。
    - `gemini.go`, `claude.go`: 具體的 Agent 實現。
- `internal/source/`: 外部任務來源適配器。
    - `github.go`: GitHub API 整合，用於獲取 Issues。
    - `dispatcher.go`: 多來源分派器。
- `internal/execution/`: 任務執行引擎。
    - `runner.go`: 控制任務執行的具體流程 (Prepare -> Run -> Judge)。
    - `judge.go`: 判別執行結果是否成功。
- `internal/gitops/`: Git 操作封裝。
- `internal/logger/`: 結構化日誌記錄工具。

### 文檔資源 (`docs/`)
- `docs/designs/`: 包含各階段 (D01-D06) 的詳細架構設計文件。開發前務必閱讀對應模組的設計。
- `docs/requirements/`: 需求規格文件。

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

- **我要新增一個 CLI 指令**: 修改 `cmd/gogents/main.go`。
- **我要調整配置參數**: 修改 `internal/foundation/config.go`。
- **我要修改 Agent 的 Prompt 或行為**: 修改 `internal/agent/` 下對應的檔案。
- **我要改變任務狀態的處理方式**: 修改 `internal/foundation/task_store.go`。
- **我要解決同步 (Sync) 失敗的問題**: 查看 `internal/source/` 下的對應適配器與 `internal/daemon/syncer.go`。
- **任務執行邏輯有誤 (Execution)**: 查看 `internal/execution/runner.go`。

## 5. 測試與品質規範 (Testing & Quality)
- **單元測試**: 所有核心邏輯必須撰寫單元測試，且 **涵蓋率 (Coverage) 至少需達到 80%**。
- **報告產出**: 單元測試結果應輸出至 `logs/unit_test_report.log`，不得直接輸出於根目錄。
- **Linting**: 提交代碼前務必通過 `mise run lint`。

---
*註：本文件應隨著專案演進持續更新。*
