# CH-003 — 移除 Gemini Agent 支援

## 1. 背景與動機

`gemini` CLI 已停止服務，其能力併入 Antigravity CLI。原本作為官方支援 Agent 之一的 Gemini（見 CH-001）已無可用的執行後端，繼續保留其 probe / executor 適配層、設定列舉值與相關文件只會誤導使用者、留下無法通過 preflight 的死路徑。

本變更將 Gemini 從系統中整體移除，使 `claude`（Claude Code）成為唯一支援的 Agent CLI 型別。Antigravity CLI 的支援不在本變更範圍內，待其介面穩定後另立變更提案評估。

## 2. 變更目標

1. 移除 `agent.type` 對 `gemini` 的支援，`claude` 成為唯一合法值。
2. 刪除 Gemini 的 probe / executor 適配層與 dispatcher 分支。
3. 清理設定校驗、preflight、測試與全部設計／需求文件中的 Gemini 敘述。
4. 重整 `README.md`：將 **Architecture** 區塊上移至 **Features** 之前，使架構圖先於功能清單呈現。

## 3. 具體需求規格

### 3.1 設定值收斂

- `runtime.yaml` 的 `agent.type` 合法值收斂為單一值 `claude`；`gemini` 不再被接受。
- 設定校驗（`internal/foundation/config.go`）：`agent.type` 的允許集合由 `{gemini, claude}` 改為 `{claude}`；填入 `gemini` 時回報設定錯誤。
- `agent.model` 語義不變（可選填，未填由 `claude` CLI 使用內部預設）。

### 3.2 移除 Gemini 適配層

- 刪除 `internal/agent/gemini.go`（`geminiProbe`、`geminiExecutor`、`NewGeminiProbe`、`NewGeminiExecutor`）。
- Dispatcher（`internal/agent/dispatcher.go`）：`NewAgentProbeFromConfig` 與 `NewAgentExecutorFromConfig` 移除 `gemini` 分支，僅保留 `claude`。
- 其他模組一律不再認識 `gemini` 特有參數（`-m`、`--yolo`、`-o stream-json`）。

### 3.3 Preflight 與執行行為

- Preflight 不再檢查 `gemini` 執行檔與 `gemini --version`；`agent.type: claude` 的檢查行為維持現行（`claude` 在 PATH、`claude --version` exit 0）。
- 移除 Gemini 特有的環境依賴敘述（`~/.gemini/`、`GEMINI_API_KEY`）。

### 3.4 測試調整

- `internal/agent/dispatcher_test.go`：移除 `gemini` 的 probe / executor 案例（D06-08 / D06-08b 對應項），保留 `claude` 與非法型別回錯的案例。
- `internal/foundation/config_test.go`：範例設定中的 `agent.type: gemini` 改為 `claude`；新增（或改寫）一則「`gemini` 現為非法值應回錯」的案例。
- `docs/tests/unit-tests.md`：D06-01 ~ D06-03（`gemini` probe 案例）改以 `claude` 描述或移除，D06-08 的列舉去除 `gemini`。

### 3.5 文件清理

以下文件移除 Gemini 敘述、將「Gemini / Claude」等雙實作描述改為僅 Claude，並保持「Agent 為可插拔、`AgentExecutor` 為介面、concrete 僅是實作」的設計語氣：

| 文件 | 調整重點 |
| ---- | -------- |
| `docs/designs/D06-agent-runner.md` | 移除 Gemini concrete（probe / executor）、子指令組裝、錯誤表與驗收敘述；concrete 實作範例改以 Claude 呈現 |
| `docs/designs/D01-foundation.md` | `agent.type` 列舉由 `gemini, claude` 改為 `claude` |
| `docs/designs/D02-daemon-sync.md` | AgentProbe 檢查範例由 `gemini --version` 改為 `claude --version` |
| `docs/designs/D03-task-execution.md` | 驗收敘述移除 `gemini` 選項 |
| `docs/designs/design-plan.md` | D06 說明由「Gemini / Claude 實作」改為「Claude 實作」 |
| `docs/requirements/reqs/R02-runtime-config.md` | 移除「Gemini executor 行為」與 Gemini preflight 檢查段；`agent.type` 支援範圍改為僅 `claude` |
| `docs/requirements/reqs/R01-workspace-layout.md` | workspace 佈局移除 `.gemini/`，agent runtime 元素以 `.claude/`、`.mcp.json` 表述 |
| `docs/requirements/reqs/R06-task-execution.md` | executor 支援型別敘述去除 `gemini` |
| `docs/requirements/requirement.md` | Agent CLI 範例去除 `gemini` |
| `AGENTS.md` | 移除 `gemini.go` 與「Gemini / Claude」雙實作敘述 |

### 3.6 README 重整：架構先於功能

`README.md` 章節順序調整，使架構圖先呈現，讀者先建立整體心智模型再看功能細節：

- 現行順序：`Features` → `Architecture`。
- 變更後順序：`Architecture` → `Features`。
- 兩區塊內容原樣搬移，僅調換位置；其餘章節（Prerequisites、Quick Start 等）順序不變。
- 同時清理 README 內全部 Gemini 敘述：
  - 開場段的 `(Gemini CLI, Claude Code)` 改為 `(Claude Code)`。
  - Features 的「Multi-Agent Support」改述為可插拔 Agent 架構，目前官方支援 Claude Code，新 Agent 可實作介面加入。
  - 架構圖中 `gemini/claude` 改為 `claude`。
  - D06 模組表描述由「Gemini/Claude probe & executor adapters」改為「Claude probe & executor adapter」。
  - Prerequisites 的 Agent CLI 由「至少其一（Gemini / Claude）」改為 Claude Code。
  - Quick Start 設定範例的 `agent.type: gemini` / `model: gemini-2.5-pro` 改為 `claude` / `claude-sonnet-5`。

## 4. 交付物

- **文件**：本變更記錄檔；`README.md` 重整；`docs/designs/` 與 `docs/requirements/` 內 Gemini 敘述清理。
- **程式碼**：
  - 刪除 `internal/agent/gemini.go`。
  - `internal/agent/dispatcher.go` 移除 `gemini` 分支。
  - `internal/foundation/config.go` 校驗集合收斂為 `{claude}`。
- **測試**：`internal/agent/dispatcher_test.go`、`internal/foundation/config_test.go` 更新；`docs/tests/unit-tests.md` 對應案例調整。

## 5. 相容性

- **破壞性變更**：既有 `agent.type: gemini` 的 `runtime.yaml` 於啟動時將因設定校驗失敗而拒啟。使用者需將 `agent.type` 改為 `claude` 並確認 `claude` CLI 已安裝授權。
- `agent.type: claude` 的既有 runtime 不受影響。
- `github_issue` / `local` 任務源、judge、runtime 註冊與觀測 CLI 皆不受影響。

## 6. 驗收標準

- `agent.type: claude` 的 `gogents start` 可通過 preflight 並正常執行任務。
- `agent.type: gemini` 的 `runtime.yaml` 於啟動時被設定校驗擋下並回報明確錯誤。
- 專案內（程式碼、設計、需求、測試、README）以不分大小寫搜尋 `gemini` 無殘留（本變更記錄檔自身的歷史敘述除外）。
- `mise run test` 全數通過。
- `README.md` 章節順序為 `Architecture` 先於 `Features`，且無 Gemini 敘述。
