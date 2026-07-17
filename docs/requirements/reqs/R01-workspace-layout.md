# R01 - Workspace 與全域目錄結構

> 主需求檔：[requirement.md](../requirement.md)

## 目的
定義 Gogents 使用的所有檔案系統路徑與目錄職責，
作為其他需求的基礎。

## Workspace 結構
單一 workspace 是一個目錄，內含一個 git repo 與 task 的
管理檔案。由 `gogents runtime create` 佈建（見 R13），亦可由使用者手動建立。
```
<workspace-root>/
├── .gogents/
│   ├── runtime.yaml              # 此 workspace 的設定（見 R02）；create 佈建
│   ├── state.yaml                # daemon 執行狀態（見 R04）
│   └── tasks/
│       └── <task-id>/
│           ├── task.yaml         # 任務狀態與中繼資料（見 R03）
│           ├── input.md          # 餵給 agent 的提示詞（見 R06）
│           ├── output.jsonl      # agent 的 stream 輸出（見 R06）
│           └── result.json       # agent 寫的結案報告（見 R07）
├── <reponame>/                   # daemon 啟動時 git clone 進來
│   ├── AGENTS.md                 # 使用者自行維護
│   └── ... (專案程式碼)
├── .claude/                      # agent runtime 元素；create 佈建，使用者可續行修改
│   ├── CLAUDE.md
│   └── skills/
└── .mcp.json                     # MCP 工具設定；create 佈建，使用者可續行修改
```

## 全域目錄結構
跨所有 workspace 共用，位於**全域家目錄根**下（下方以預設值 `~/.gogents/` 表示）。
```
~/.gogents/
├── config.yaml                   # 全域設定（見 R02）
├── tickets/                      # local 任務源的預設 ticket inbox（見 R11）
│   └── <ticket-id>.yaml          # 由 producer 建立；gogents 回寫終態
└── logs/
    └── <workspace-hash>/
        ├── daemon.jsonl          # daemon 執行日誌（見 R09）
        └── error.jsonl           # daemon 錯誤日誌（見 R09）
```

`tickets/` 僅在 `source.type: local` 且 `source.path` 省略時作為預設 inbox；顯式設定 `source.path` 時 inbox 落在該路徑，不在全域家目錄下。gogents 不建立此目錄（見 R11）。

`<workspace-hash>` 為 workspace 絕對路徑的短雜湊（建議 SHA1 前 8 碼），用來區分不同 workspace 的 daemon 日誌，避免互相覆蓋。

### 全域家目錄根解析（`GOGENTS_HOME`）

全域家目錄根由單一來源解析，`config.yaml` 與 `logs/` 皆據此推導：

1. 環境變數 `GOGENTS_HOME` 非空 → 逕用其值。
2. 否則 → `$HOME/.gogents`（預設，向後相容）。
3. `$HOME` 亦無法解析時 → `/tmp` fallback。

`GOGENTS_HOME` 讓同一台機器可各持獨立的全域註冊表與日誌（例如 dev 綁 sandbox、prod 綁營運目錄互不污染），與 agent-manager (am) 的 `AM_HOME` 對稱。

**per-workspace 的 `.gogents/`**（`runtime.yaml` / `state.yaml` / `tasks/`）位於 workspace 目錄之下、屬相對路徑，**不受 `GOGENTS_HOME` 影響**，不隨全域家目錄根移動。

## 任務代號（task-id）格式

```
github-<issue-number>
```

範例：`github-42`

- 由於單一 workspace 只綁一個 repo，owner/repo 資訊已在
  runtime.yaml 中，task-id 不重複夾帶
- owner/repo/branch/PR URL 等資訊存放在該任務的 task.yaml

## 路徑變動責任界線
- **`runtime create` 責任**（見 R13）：
  - 建立 workspace 根目錄
  - 產生 `.gogents/runtime.yaml`
  - 佈建 agent runtime 元素（`.claude/CLAUDE.md`、`.claude/skills/`、`.mcp.json`）
- **使用者責任**：
  - 決定 workspace 位置
  - 佈建後續行調整 agent runtime 元素；不經 `runtime create` 時則自行建立上述全部
- **Daemon 責任**：
  - 建立 `.gogents/tasks/`、`.gogents/state.yaml`
  - `git clone` repo 到 workspace 根目錄（見 R04）
  - 建立 `~/.gogents/logs/<workspace-hash>/`

## 路徑衝突處理
- 若 `.gogents/runtime.yaml` 不存在：daemon 拒絕啟動
- 若 `<reponame>/` 已存在且為有效 git repo：daemon 略過 clone，
  僅執行 `git fetch`
- 若 `<reponame>/` 已存在但非 git repo：daemon 拒絕啟動並回報錯誤

## 相關需求
- R13 - Workspace 佈建（`runtime create` 如何產生上述結構）
- R02 - Runtime 設定檔（定義 runtime.yaml 與 config.yaml 內容）
- R03 - 任務狀態模型（定義 task.yaml 內容）
- R04 - Daemon 生命週期（定義 state.yaml 內容與 clone 時機）
- R09 - 日誌規範（定義 logs 目錄使用方式）
