# CH-001 — 支援 Claude Code Agent CLI

## 1. 背景與動機

目前專案主要使用的 `gemini` CLI 在特定環境或長時運行下可能出現不穩定或模型配額問題。為了提升任務執行的成功率與系統韌性，並落實 D06 設計文件中「支援多種 Agent 客戶端」的承諾，本變更旨在引入 Anthropic 的 `claude` (Claude Code) 作為第二個官方支援的 Agent CLI 型別。

## 2. 變更目標

1. 在 `agent.type` 設定中增加對 `claude` 的支援。
2. 實作符合 `AgentProbe` 與 `AgentExecutor` 介面的 Claude 適配層。
3. 確保 `claude` CLI 的子行程管理（I/O 重導、工作目錄、中斷處理）符合 R06 與 D06 規範。

## 3. 具體需求規格

### 3.1 設定值擴充
- `runtime.yaml` 內的 `agent.type` 現在支援合法值：`claude`。
- `agent.model` 可選填（若不填則由 `claude` CLI 使用其內部預設值）。

### 3.2 Agent 探測 (Probe)
- **路徑檢查**：確認 `claude` 執行檔位於系統 `PATH` 中。
- **版本檢查**：執行 `claude --version` 並確認回傳值為 0。
- **逾時與重試**：比照 Gemini，逾時設為 10 秒。

### 3.3 Agent 執行 (Executor)
- **命令組裝**：
    - 基本格式：`claude`
    - 命令應支援非互動模式（視 `claude` CLI 參數而定，確保不卡死在 stdin 詢問）。
    - 範例：`claude --non-interactive < input.md` (具體 flag 依 Claude Code CLI 為準)。
- **工作目錄**：設置為 `req.WorkDir` (repository root)。
- **I/O 管理**：
    - Stdin：接 `input.md`。
    - Stdout/Stderr：合併重導至 `output.jsonl` (Truncate)。
- **模型傳遞**：若 `req.Model` 非空，應轉化為 `claude` 對應的參數。

### 3.4 Dispatcher 註冊
- 修改 `internal/agent/dispatcher.go`：
    - 在 `NewAgentProbeFromConfig` 增加 `claude` 字串的 mapping。
    - 在 `NewAgentExecutorFromConfig` 增加 `claude` 字串的 mapping。

## 4. 交付物

- **文件更新**：本變更記錄檔。
- **程式碼**：
    - `internal/agent/claude/` 封裝包。
    - `internal/agent/dispatcher.go` 註冊邏輯更新。

## 5. 驗收標準 (Acceptance Criteria)

- **Probe 驗證**：將 `runtime.yaml` 改為 `agent.type: claude` 後，`gogents start` 可通過 Preflight 檢查。
- **執行驗證**：觸發任務後，可正確拉起 `claude` CLI。
- **日誌驗證**：`daemon.jsonl` 中出現 `task.agent_invoked` 與 `task.agent_exited` 事件且 `agent_type` 為 `claude`。
- **結果驗證**：`output.jsonl` 正確記錄 `claude` 的輸出流。
