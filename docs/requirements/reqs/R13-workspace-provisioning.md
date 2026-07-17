# R13 - Workspace 佈建

> 主需求檔：[requirement.md](../requirement.md)

## 目的

定義 `gogents runtime create`：由內建 template 佈建出一個可直接 `gogents start` 的 workspace（`.gogents/runtime.yaml` 與 agent runtime 元素），使使用者無需手寫 runtime.yaml、無需自備 agent 執行環境配置即可起用一個 runtime。

## 指令契約

```
gogents runtime create <name>
    [--workspace <path>]
    --repo <url> [--branch <branch>]
    --agent <type> --model <model>
    --source <local|github_issue> [--label <label>]
    [--instance <owner/repo>] [--tickets-dir <path>] [--force]
```

| 參數 | 必填 | 預設 | 對應 runtime.yaml 欄位 |
| --- | --- | --- | --- |
| `<name>` | 是 | — | `runtime_name` |
| `--workspace` | 否 | `./<name>` | —（workspace 根目錄本身） |
| `--repo` | 是 | — | `project.repo.url` |
| `--branch` | 否 | `main` | `project.repo.default_branch` |
| `--agent` | 是 | — | `agent.type` |
| `--model` | 是 | — | `agent.model` |
| `--source` | 是 | — | `source.type` |
| `--label` | 否 | `<name>` | `source.filter.label` |
| `--instance` | `--source=github_issue` 時必填 | — | `source.instance` |
| `--tickets-dir` | 否 | 省略 | `source.path`（省略時不寫入） |
| `--force` | 否 | false | —（跳過非空目錄確認） |

**name 與 workspace 各自獨立**：同一種專長服務不同 repo 時，以不同 `<name>`、`--workspace`、`--repo` 各跑一次 `create`，得到彼此獨立的 runtime。

**`--workspace` 不預設到全域家目錄根底下**：全域家目錄承載註冊表與日誌（狀態目錄），不混使用者工作區；工作區位置由使用者決定。

**`--tickets-dir` 省略時不寫入 `source.path`**，由 daemon 側的預設 inbox 解析接手（見 R11）。

**不引入 specialty／tier 概念**：`agent.type` 與 `agent.model` 由旗標明確指定，無「專長 × 分級 → 模型」查表。

## 佈建範圍

`create` 產出的 workspace 包含（結構見 R01）：

| 產出 | 來源 |
| --- | --- |
| `.gogents/runtime.yaml` | template 的 `runtime.yaml.tmpl` 經參數展開 |
| `.claude/CLAUDE.md`、`.claude/skills/` | template 原樣複製 |
| `.mcp.json` | template 原樣複製 |

**只建 workspace，不註冊**：註冊表 `config.yaml` 的寫入時機維持在 `gogents start`（見 R12）；`create` 完成後 `runtime list` 看不到該 runtime，直到 `start`。

**不 clone repo**：`<reponame>/` 的 clone 由 daemon 啟動序列負責（見 R01、R04）；`create` 只把 repo URL 寫進 runtime.yaml。

`create` 與 `runtime remove` **並非逆操作**：`create` 建 workspace 目錄，`remove` 只反註冊、不刪使用者的 workspace。

## Template

- gogents 內建一份 template，打包進 binary，使只取得 release binary 的使用者無需任何外部檔案即可佈建。repo 內同時保留原始檔，供 clone 後複製修改。
- **軸為專長，不含 source**：`source` 是 runtime.yaml 的欄位，由 `--source` 於展開時條件決定；依來源分軸會產生「專長 × 來源」的笛卡兒積，而兩者差異僅 runtime.yaml 數行。
- template 內 `CLAUDE.md` 的措辭為**來源中性**（以「任務的目標與脈絡」表述，不提 issue 或 ticket 檔）；兩種來源的 runtime.yaml 差異以 README 範例說明，不做成 template。

## 目標目錄規則

| 目標 workspace 狀態 | 行為 |
| --- | --- |
| 不存在 | 建立後佈建 |
| 存在且為空目錄 | 佈建 |
| 存在但非目錄 | 拒絕、非零 exit（`--force` 亦不繞過） |
| 存在且為非空目錄 | 需確認：`--force` 直接佈建；互動終端列出將寫入／覆蓋的檔案並提示，同意才佈建；非互動終端一律拒絕並提示改用 `--force` |

**非互動時不預設同意**：CI／腳本情境下靜默覆蓋使用者目錄的風險高於直接失敗。

## 不變量

- **gogents 永不刪除使用者目錄下的任何檔案**：佈建只新增檔案與覆蓋同名檔案，workspace 內其餘檔案一律不動；佈建中途失敗時不做任何回滾刪除，保留已寫入的檔案並於錯誤訊息列出。
- 確認被拒或目標檢查失敗時，**不寫入任何內容**。
- 產出的 workspace 必須能直接 `gogents start --workspace <path>`：`runtime.yaml` 通過 R02 的 schema 驗證（含依 `source.type` 的條件必填欄位）。
- **重跑收斂**：同參數對同一 workspace 重跑 `create`（經確認或 `--force`），以相同內容覆蓋同名檔案，結果收斂至同一狀態；非空目錄不會被靜默覆蓋。

## 錯誤情境

| 情境 | 處理 |
| --- | --- |
| 必填旗標缺漏／`--source`、`--agent` 不在列舉值 | usage 訊息、非零 exit |
| `--source=github_issue` 但無 `--instance` | 錯誤訊息指出條件必填、非零 exit |
| `--instance` 用於 `local`／`--tickets-dir` 用於 `github_issue` | 錯誤訊息指出旗標與 source 不相容、非零 exit |
| 目標非目錄／非空且未經確認 | 明確錯誤、非零 exit、不寫入 |
| 確認被拒 | 非零 exit、不寫入（使用者主動中止，非錯誤語意） |
| template 展開失敗、寫入失敗 | 明確錯誤、非零 exit；已寫入檔案保留並列出 |

`create` 為短命 CLI：錯誤寫 stderr + 非零 exit，不寫 daemon 日誌。本子指令不提供 `--json`（`--json` 的對象是程式化查詢，見 R12）。

## 相關需求

- R01 - Workspace 與全域目錄結構（產出的目錄佈局、路徑責任界線）
- R02 - Runtime 設定檔（產出的 runtime.yaml schema 與必填欄位）
- R11 - 本地任務源（`--tickets-dir` 對應的 `source.path` 與預設 inbox）
- R12 - Runtime 註冊與觀測（註冊時機、`runtime remove` 的界線）
