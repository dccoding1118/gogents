# R13 - Workspace 佈建

> 主需求檔：[requirement.md](../requirement.md)

## 目的

定義 `gogents runtime create`：由 template 佈建出一個可直接 `gogents start` 的 workspace（`.gogents/runtime.yaml` 與 agent runtime 元素），使使用者無需手寫 runtime.yaml、無需自備 agent 執行環境配置即可起用一個 runtime。內建 template 為預設來源，使用者亦可指定自己的 template 樹，把一種專長固化成可重複佈建的起手式。

## 指令契約

```
gogents runtime create <name>
    [--template <path|name>]
    [--workspace <path>]
    --repo <url> [--branch <branch>]
    --agent <type> --model <model>
    --source <local|github_issue> [--label <label>]
    [--instance <owner/repo>] [--tickets-dir <path>] [--force]
```

| 參數 | 必填 | 預設 | 對應 runtime.yaml 欄位 |
| --- | --- | --- | --- |
| `<name>` | 是 | — | `runtime_name` |
| `--template` | 否 | 省略 | —（template 來源，見 §Template；省略時用內建 template） |
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

以內建 template 佈建的 workspace 包含（結構見 R01）：

| 產出 | 來源 |
| --- | --- |
| `.gogents/runtime.yaml` | template 的 `runtime.yaml.tmpl` 經參數展開 |
| `.claude/CLAUDE.md`、`.claude/skills/` | template 原樣複製 |
| `.mcp.json` | template 原樣複製 |

指定外部 template 時，產出由該樹決定（見 §Template 樹契約）。

**只建 workspace，不註冊**：註冊表 `config.yaml` 的寫入時機維持在 `gogents start`（見 R12）；`create` 完成後 `runtime list` 看不到該 runtime，直到 `start`。

**不 clone repo**：`<reponame>/` 的 clone 由 daemon 啟動序列負責（見 R01、R04）；`create` 只把 repo URL 寫進 runtime.yaml。

`create` 與 `runtime remove` **並非逆操作**：`create` 建 workspace 目錄，`remove` 只反註冊、不刪使用者的 workspace。

## Template

### 來源解析

template 樹的來源有三個，彼此**互斥**：依下表由上而下選出**唯一一棵**樹，選定即不再參考其餘來源。

| 情境 | 來源樹 |
| --- | --- |
| `--template` 為路徑形式 | 該路徑對應的目錄（`GOGENTS_TEMPLATES` 是否設定皆不影響） |
| `--template` 為名稱形式、且 `GOGENTS_TEMPLATES` 已設定 | `$GOGENTS_TEMPLATES/<name>/` |
| `--template` 為名稱形式、且 `GOGENTS_TEMPLATES` 未設定 | `<當前目錄>/<name>/` |
| 未給 `--template` | 內建 template |

`--template` 的值含路徑分隔符即為**路徑形式**，否則為**名稱形式**；判別只看值的字面形式，不看目錄是否存在。要指定當前目錄下的樹又繞過 `GOGENTS_TEMPLATES`，寫 `./<name>`。

**不做來源疊加**：選定的樹即全部內容，不與內建 template 合併。只改一個檔也需複製整棵內建 template 後修改——release 產物與 repo 都帶著原始檔，複製成本近乎零，換得「看到什麼樹就落什麼檔」的可預測性。

**無名稱回退**：名稱形式解析不出目錄即報錯，不改試其他名稱、不落回內建 template。內建 template 只在完全未給 `--template` 時生效，不參與名稱解析；其名稱不具特殊意義。

**`GOGENTS_TEMPLATES` 是名稱形式的展開基底，不是搜尋路徑**：單一路徑值（非 `PATH` 式多路徑串接），且只在名稱形式下生效——設了變數但未給 `--template` 仍用內建 template。

**外部來源失敗一律報錯，不落回內建 template**：使用者明示 `--template` 就是要那棵樹；靜默落回會產出一個「看似成功、內容卻不是他要的」workspace，比失敗更難察覺。

### 內建 template

- gogents 內建一份 template，打包進 binary，使只取得 release binary 的使用者無需任何外部檔案即可佈建。repo 內同時保留原始檔，供複製修改。
- **軸為專長，不含 source**：`source` 是 runtime.yaml 的欄位，由 `--source` 於展開時條件決定；依來源分軸會產生「專長 × 來源」的笛卡兒積，而兩者差異僅 runtime.yaml 數行。
- 內建 template 的 `CLAUDE.md` 措辭為**來源中性**（以「任務的目標與脈絡」表述，不提 issue 或 ticket 檔）；兩種來源的 runtime.yaml 差異以 README 範例說明，不做成 template。

### 樹契約

外部樹與內建 template 同構，適用同一套展開規則：

- **指定的目錄本身即樹根**，其下的相對路徑就是 workspace 內的相對路徑，不做任何路徑映射，樹根與 workspace 根之間不存在額外的中介層。
- 可引用的展開變數即指令參數對應的資料模型欄位；引用未知欄位於展開時報錯。
- **不驗證樹的內容**：樹內缺 `runtime.yaml.tmpl`、或展開出的 runtime.yaml 不合 schema，`create` 皆不預先攔截——展開是無寫死單檔邏輯的通用規則，且外部樹的用途由使用者定義。後果由 `gogents start` 的既有 schema 驗證呈現。
- **只處理一般檔案與目錄**：外部樹可能含內建 template 不可能產生的項目（symlink、device 等），一律報錯，不靜默略過——略過會產出看似成功、卻缺檔的 workspace。

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
- 確認被拒、目標檢查失敗或 template 來源解析失敗時，**不寫入任何內容**。
- **以內建 template 佈建的 workspace 必須能直接 `gogents start --workspace <path>`**：`runtime.yaml` 通過 R02 的 schema 驗證（含依 `source.type` 的條件必填欄位）。此擔保的對象是內建 template；外部樹的正確性歸使用者（見 §Template 樹契約）。
- **重跑收斂**：同參數對同一 workspace 重跑 `create`（經確認或 `--force`），以相同內容覆蓋同名檔案，結果收斂至同一狀態；非空目錄不會被靜默覆蓋。

## 錯誤情境

| 情境 | 處理 |
| --- | --- |
| 必填旗標缺漏／`--source`、`--agent` 不在列舉值 | usage 訊息、非零 exit |
| `--source=github_issue` 但無 `--instance` | 錯誤訊息指出條件必填、非零 exit |
| `--instance` 用於 `local`／`--tickets-dir` 用於 `github_issue` | 錯誤訊息指出旗標與 source 不相容、非零 exit |
| `--template` 解析出的目錄不存在或非目錄 | 明確錯誤、非零 exit、不寫入；訊息列出實際查找的絕對路徑與其來自 `GOGENTS_TEMPLATES` 或當前目錄 |
| `--template` 值為空字串 | 旗標驗證錯誤、非零 exit；不等同省略 |
| 外部樹含非一般檔案項目 | 明確錯誤、非零 exit；已寫入檔案保留並列出 |
| 目標非目錄／非空且未經確認 | 明確錯誤、非零 exit、不寫入 |
| 確認被拒 | 非零 exit、不寫入（使用者主動中止，非錯誤語意） |
| template 展開失敗、寫入失敗 | 明確錯誤、非零 exit；已寫入檔案保留並列出 |

`create` 為短命 CLI：錯誤寫 stderr + 非零 exit，不寫 daemon 日誌。本子指令不提供 `--json`（`--json` 的對象是程式化查詢，見 R12）。

## 相關需求

- R01 - Workspace 與全域目錄結構（產出的目錄佈局、路徑責任界線）
- R02 - Runtime 設定檔（產出的 runtime.yaml schema 與必填欄位）
- R11 - 本地任務源（`--tickets-dir` 對應的 `source.path` 與預設 inbox）
- R12 - Runtime 註冊與觀測（註冊時機、`runtime remove` 的界線）
