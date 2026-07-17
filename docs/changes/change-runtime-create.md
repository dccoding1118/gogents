# CH-007 — `gogents runtime create`（workspace 佈建）

## 1. 背景與動機

gogents 的定位是**個人／本機 agent 的專業運作環境工具**：使用者以一個 workspace 承載一個 agent runtime，daemon 依 ticket 認領、執行、回報。但目前 gogents **沒有建立 workspace 的能力**——R01 §路徑變動責任界線把「建立 workspace 根目錄、放置 `.gogents/runtime.yaml`、配置 `.claude/` 與 `.mcp.json`」全歸在使用者責任，實務上使用者得手寫一份 runtime.yaml 並自備 agent 執行環境元素，才能跑第一個 `gogents start`。

現況能自動佈建 workspace 的只有 am：am 的 `internal/provision` 以自帶的 `templates/<specialty>/` materialize 出 worker workspace。但那是 am 專有路徑——gogents 對外公開後，使用者若不裝 am 就沒有任何開箱可用的起手式，而 am 的 provision 也綁著 specialty／tier 查表與 pid 管理等 am 控制面概念，不適合原樣搬進 gogents。

本變更讓 gogents 自備 workspace 佈建能力：`gogents runtime create` 一行建出可直接 `start` 的 workspace。

## 2. 變更目標

1. 新增 `internal/provision` 套件：由 template 樹 materialize 出一個 workspace（copy tree + `text/template` render + 非空目錄確認 + 不刪除既有內容）。
2. 新增 `gogents runtime create <name>` CLI 子指令。
3. 新增內建 template `templates/base/`，以 `go:embed` 打包進 binary，使只下載 release binary 的使用者無需任何外部檔案即可佈建。
4. 更新 R01 的路徑責任界線：workspace 元素改由 `runtime create` 生成，使用者不再需要手寫 runtime.yaml。

**不在本變更範圍**：

- **template 外部化**（`--template` 路徑／`GOGENTS_TEMPLATES` 解析順序）→ CH-008。本變更內建 embed 為**唯一**來源；CH-008 的外部來源是疊在 embed 之上的**選用覆寫**（`--template` → `GOGENTS_TEMPLATES/<name>` → embed），embed 永遠是保底來源。
- **local source 的 inbox 預設路徑**（`~/.am/tickets` → `<GlobalHome>/tickets`）→ CH-009。
- am 側收斂（改呼叫 `gogents runtime create`、刪除 am 的 `internal/provision` 與 `templates/`）→ 待 CH-007／CH-008 落地後另案處理；**本變更不動 am**。

## 3. 具體需求規格

### 3.1 指令簽名

```
gogents runtime create <name>
    [--workspace <path>]
    --repo <url> [--branch <branch>]
    --agent <type> --model <model>
    --source <local|github_issue> [--label <label>]
    [--instance <owner/repo>] [--tickets-dir <path>] [--force]
```

| 參數 | 必填 | 預設 | 說明 |
| --- | --- | --- | --- |
| `<name>` | 是 | — | runtime 名，位置參數（與 `runtime status/remove` 的 target 形式一致）；寫入 `runtime_name` |
| `--workspace` | 否 | `./<name>` | workspace 根目錄 |
| `--repo` | 是 | — | `project.repo.url` |
| `--branch` | 否 | `main` | `project.repo.default_branch` |
| `--agent` | 是 | — | `agent.type`；目前僅 `claude` |
| `--model` | 是 | — | `agent.model` |
| `--source` | 是 | — | `source.type`：`local` 或 `github_issue` |
| `--label` | 否 | `<name>` | `source.filter.label` |
| `--instance` | `--source=github_issue` 時必填 | — | `source.instance`（`owner/repo`）；`local` 時給定即報錯 |
| `--tickets-dir` | 否 | 省略 | `source.path`；僅 `local` 有意義，`github_issue` 時給定即報錯 |
| `--force` | 否 | false | 跳過非空目錄的互動確認（§3.2 C4） |

**name 與 workspace 各自獨立**：同一份 template、同一種專長要服務不同 repo 時，跑兩次 `create`（不同 `<name>`、不同 `--workspace`、不同 `--repo`）即得兩個獨立 runtime。

**`--workspace` 不預設到 `GOGENTS_HOME` 底下**：全域家目錄是註冊表與日誌的狀態目錄，不混使用者工作區；工作區位置由使用者決定（預設就在當前目錄建 `<name>/`）。

**`--tickets-dir` 省略時 `source.path` 不寫入 runtime.yaml**，由 daemon 側的預設 inbox 解析接手（現況見 `internal/source/local.go`；該預設值本身由 CH-009 調整，與本變更無相依）。

**不引入 specialty／tier 概念**：`agent.type` 與 `agent.model` 由旗標明確指定，無 `specialty@tier → model` 查表。

### 3.2 `runtime create` 行為契約

| 編號 | 契約 |
| --- | --- |
| C1 | 解析旗標 → 驗證（必填、enum、per-source 條件必填）→ 目標目錄檢查（C4）→ materialize workspace。失敗一律 exit ≠ 0。 |
| C2 | **只建 workspace，不註冊**。註冊表 `config.yaml` 的寫入時機維持在 `gogents start`（D08 B1）。`create` 完成後 `runtime list` 看不到該 runtime，直到 `start`。 |
| C3 | **不 clone repo**。`<reponame>/` 的 clone 仍由 daemon 啟動序列負責（R01／R04），`create` 只寫入 `project.repo.url`。 |
| C4 | **非空目錄需確認**：目標 workspace 不存在或為空目錄 → 直接進行；已存在且為**非空目錄** → 列出將寫入／覆蓋的檔案並要求互動確認，同意才進行、否則中止且不寫入任何內容；已存在但**非目錄**（一般檔案等）→ 一律拒絕、不確認。 |
| C5 | **不刪除既有內容**：materialize 只新增檔案與覆蓋同名檔案，workspace 內其餘檔案一律不動。中途失敗時**不做任何回滾刪除**，保留已寫入的檔案並於錯誤訊息列出，由使用者自行決定如何處理。 |
| C6 | 產出的 workspace 必須能直接 `gogents start --workspace <path>`：`runtime.yaml` 通過 `foundation` 的 schema 驗證（含 per-source 必填欄位）。 |

`create` 與 `runtime remove` **並非逆操作**，屬刻意設計：`create` 建 workspace 目錄，`remove` 只反註冊、不刪使用者的 workspace。C5 與此一致——**gogents 永不刪除使用者目錄下的任何檔案**，包含失敗路徑。

**「idempotent」的語意界定**：指「重跑不產生額外副作用」——同參數對同一 workspace 重跑 `create`，經確認後會以相同內容覆蓋同名檔案，結果收斂到同一狀態。它**不**表示靜默重跑：非空目錄一律需經 C4 確認（或 `--force`）。

### 3.3 非空目錄確認流程

```
target 狀態:
  不存在            → MkdirAll 後 materialize
  存在且為空目錄    → materialize
  存在但非目錄      → ErrWorkspaceExists，exit ≠ 0（--force 亦不繞過）
  存在且為非空目錄  →
      --force            → materialize
      stdin 非互動終端   → ErrWorkspaceNotEmpty，exit ≠ 0，訊息提示改用 --force
      互動終端           → 列出將寫入／覆蓋的檔案 → 提示 → 讀 stdin
                            y/yes → materialize
                            其他  → 中止，exit ≠ 0，不寫入任何內容
```

提示訊息需明確表達後果：目錄非空，將寫入 agent 運作環境配置（列出 template 展開後的目標路徑清單，標示哪些為既有檔案的覆蓋），其餘檔案不受影響。

**非互動時不預設 yes**：CI／腳本情境下靜默覆蓋使用者目錄的風險高於失敗，因此非 TTY 一律要求明示 `--force`。

### 3.4 內建 template `templates/base/`

**軸為專長，不含 source**：`source` 是 runtime.yaml 欄位，由 `--source` 在 render 時條件展開；依來源分軸會產生「專長 × 來源」笛卡兒積，而差異僅 runtime.yaml 數行。本變更只出一份 `templates/base/`。

**template 樹內的相對路徑即 workspace 內的相對路徑**，materialize 不做任何路徑映射：

```
templates/base/                    →  <workspace>/
├── .gogents/
│   └── runtime.yaml.tmpl          →  .gogents/runtime.yaml   （唯一會 render 的檔案）
├── .claude/
│   ├── CLAUDE.md                  →  .claude/CLAUDE.md
│   └── skills/.gitkeep            →  .claude/skills/
└── .mcp.json                      →  .mcp.json
```

repo 內保留原始檔（供 clone 後複製修改），同時以 `go:embed` 打包進 binary。`.` 開頭的目錄需以 `all:` 前綴嵌入（`//go:embed all:templates/base`），否則 `.gogents/`、`.claude/`、`.mcp.json` 會被 embed 預設規則忽略。

`.claude/CLAUDE.md` 的措辭需**來源中性**（以「任務的目標與脈絡」表述，不提 issue／ticket 檔），不為 `local` 與 `github_issue` 各出一份；兩種來源的差異以 README 的 runtime.yaml 範例說明，不做成 template。

### 3.5 materialize 規則

走訪 template 樹，逐一落到 workspace 對應相對路徑：

| 規則 | 行為 |
| --- | --- |
| 目錄 | 以 `0o750` 建立對應目錄（已存在則沿用） |
| `*.tmpl` 檔 | 經 `text/template` render，去掉 `.tmpl` 後綴後原子寫入 |
| 其他檔 | 純複製，不 render |
| 同名既有檔 | 覆蓋（已經過 §3.3 確認） |
| 檔案權限 | 一律 `0o600`（不保留來源權限位元） |
| 寫入方式 | 原子寫入（write-tmp → fsync → rename），與 `GlobalConfigStore` 的 upsert 風格一致 |

**通用規則、無寫死單檔邏輯**：新增 template 檔案不需改 `internal/provision` 的程式碼，這也讓 CH-008 的外部 template 只需複製 `templates/base/` 修改即可。

`text/template` 以 `Option("missingkey=error")` 執行：template 引用不存在的欄位直接報錯，不 silently 留空。

### 3.6 template 資料模型

| 欄位 | 型別 | 來源 |
| --- | --- | --- |
| `Name` | string | `<name>` |
| `RepoURL` | string | `--repo` |
| `Branch` | string | `--branch`（預設 `main`） |
| `Agent` | string | `--agent` |
| `Model` | string | `--model` |
| `Source` | string | `--source` |
| `Label` | string | `--label`（預設 `<name>`） |
| `Instance` | string | `--instance`（`local` 時為空字串） |
| `TicketsPath` | string | `--tickets-dir`（省略時為空字串） |

`runtime.yaml.tmpl` 的條件展開：`source.instance` 僅在 `Source == "github_issue"` 時輸出；`source.path` 僅在 `TicketsPath` 非空時輸出。固定值（不進資料模型）：`source.sync_interval: 30s`、`task_policy.max_failures: 3`。

### 3.7 錯誤處理

| 情境 | 處理 |
| --- | --- |
| 必填旗標缺漏／`--source`／`--agent` 不在 enum | usage 訊息、exit ≠ 0 |
| `--source=github_issue` 但無 `--instance` | 錯誤訊息指出條件必填、exit ≠ 0 |
| `--instance` 用於 `local`／`--tickets-dir` 用於 `github_issue` | 錯誤訊息指出旗標與 source 不相容、exit ≠ 0 |
| workspace 路徑存在但非目錄 | `ErrWorkspaceExists`、exit ≠ 0、不寫入 |
| workspace 非空且非互動終端、未給 `--force` | `ErrWorkspaceNotEmpty`、exit ≠ 0、不寫入；訊息提示 `--force` |
| 非空目錄確認被拒 | exit ≠ 0、不寫入；非錯誤語意（使用者主動中止） |
| template render 失敗（欄位缺漏／語法錯） | `ErrTemplateInvalid`、exit ≠ 0；已寫入的檔案保留並列出（C5） |
| 寫入失敗（磁碟／權限） | `ErrIO`、exit ≠ 0；已寫入的檔案保留並列出（C5） |

`create` 為短命 CLI：錯誤寫 stderr + 非零 exit，不寫 daemon 日誌。本子指令**不提供 `--json`**（`--json` 契約的對象是 am 的程式化查詢，見 D08 §3.4）。

## 4. 交付物

**文件先行**：以下 canonical 文件於實作前先更新到最新狀態。

| 落點 | 內容 |
| --- | --- |
| `docs/requirements/reqs/R13-workspace-provisioning.md` | 新需求：workspace 佈建（指令契約、非空目錄確認、不刪除不變量、template 職責） |
| `docs/requirements/reqs/R01-workspace-layout.md` | 路徑責任界線更新（workspace 元素由 `runtime create` 生成） |
| `docs/designs/D09-provision.md` | 新模組設計：materialize、template 樹、資料模型、error sentinels |
| `docs/designs/D08-runtime-cli.md` | 新增 `runtime create` 子指令契約與參數表 |
| `docs/tests/unit-tests.md` | 新增 provision 與 create CLI 的案例清單 |
| `AGENTS.md`、`README.md` | 模組導覽與快速開始改用 `runtime create` |

**程式落點**：

| 落點 | 內容 |
| --- | --- |
| `internal/provision/` | `Materialize`（§3.5）、目標目錄狀態判定、error sentinels（`ErrWorkspaceExists` / `ErrWorkspaceNotEmpty` / `ErrTemplateInvalid` / `ErrIO`）、embed FS 存取 |
| `templates/base/` | §3.4 的 template 樹 |
| `internal/cli/` | `runtime create` 旗標解析、驗證、確認提示、輸出；`Dispatch` 的 `runtime` 分支加 case |

單元測試涵蓋：materialize 各檔正確落地（含 `.tmpl` 去後綴與 render 值）、`source` 條件展開（`local` 無 `instance`／`github_issue` 有 `instance`；`--tickets-dir` 有無時 `source.path` 的存在性）、`--label` 預設為 `<name>`、空目錄與不存在目錄放行、非空目錄未確認時不寫入、`--force` 跳過確認、非目錄佔位拒絕、失敗時既有檔案不被刪除、產出的 runtime.yaml 通過 `foundation` schema 驗證、旗標驗證的各錯誤路徑。

## 5. 相容性

- **`runtime create` 為新增子命令**：`start` / `list` / `status` / `remove` / `logs` 既有行為與輸出完全不變。
- **runtime.yaml schema 不變**：本變更只是生成既有 schema 的檔案，不新增／改名欄位；手寫的既有 workspace 不受影響。
- **註冊表契約不變**：`create` 不寫 config.yaml，`runtime list` 的內容語意（已 `start` 過的 runtime）維持不變。
- **am 不受影響**：am 續用自己的 `internal/provision` 與 `templates/`，本變更未觸碰 am 的任何路徑或契約。兩系統的介面是 **runtime.yaml schema**，不是 template 佈局——gogents 的 template 佈局按自身需要設計，不對齊 am 的 `templates/` 結構。

## 6. 關聯

- 現況責任界線：R01 §路徑變動責任界線；runtime.yaml schema：`internal/foundation/config.go`、R02。
- CLI 分派與既有子指令契約：D08、`internal/cli/cli.go`。
- 後續：CH-008（template 外部化）相依本變更的 `internal/provision` 與 `templates/base/`；CH-009（local source 去 am 化）與本變更無相依，可並行。
