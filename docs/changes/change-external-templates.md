# CH-008 — template 外部化（`--template` / `GOGENTS_TEMPLATES`）

## 1. 背景與動機

CH-007 讓 `gogents runtime create` 由內建 template 佈建 workspace，`templates/base/` 以 `go:embed` 打包進 binary，是**唯一**來源（`internal/cli/create.go` 寫死 `src := provision.Builtin()`）。

內建 template 保證「只下載 release binary 也能開箱佈建」，但它是一份通用範本：`.claude/CLAUDE.md` 的措辭來源中性、`.claude/skills/` 為空、`.mcp.json` 無任何 server。使用者一旦要把某個專長（工作慣例、skills、MCP server 組合）固化成可重複佈建的起手式，目前只能 `create` 完再手動改一輪，改動無法複用到下一個 workspace——**gogents 的 template 目前不可替換**。

本變更讓使用者指定自己的 template 樹：`runtime create` 讀哪一棵樹由旗標與環境變數決定，embed 退為未指定時的保底來源。

## 2. 變更目標

1. `runtime create` 新增 `--template` 旗標，接受路徑或名稱兩種形式。
2. 新增 `GOGENTS_TEMPLATES` 環境變數，作為名稱形式的展開基底。
3. `internal/provision` 新增外部 template 樹解析，embed 由唯一來源改為未指定 `--template` 時的來源。

**不在本變更範圍**：

- **`template eject`（把內建 template 匯出到檔案系統）**：release 產物已帶 `templates/`、GitHub 網頁也可直接翻閱，取得範本的需求已被覆蓋；僅「以 `go install` 安裝且不開網頁」的窄情境有用，暫不做。
- **template 樹的內容驗證**（例如檢查是否含 `runtime.yaml.tmpl`）：見 §3.5。
- **local source 的 inbox 預設路徑**（`~/.am/tickets` → `<GlobalHome>/tickets`）→ CH-009。
- am 側收斂（改呼叫 `gogents runtime create`、刪除 am 的 `internal/provision` 與 `templates/`）→ 待後續另案處理；**本變更不動 am**。

## 3. 具體需求規格

### 3.1 指令簽名

`runtime create` 於 CH-007 的簽名上新增一個選用旗標，其餘參數與行為不變：

```
gogents runtime create <name>
    [--template <path|name>]
    [--workspace <path>]
    --repo <url> [--branch <branch>]
    ...
```

| 參數 | 必填 | 預設 | 說明 |
| --- | --- | --- | --- |
| `--template` | 否 | 省略 | template 樹的路徑或名稱（§3.2）；省略時使用內建 embed |

### 3.2 template 來源解析

三個來源**互斥**：依下表由上而下選出**唯一一棵**樹，選定即不再參考其餘來源。

| 情境 | 來源樹 |
| --- | --- |
| `--template` 為路徑形式 | 該路徑對應的目錄（`GOGENTS_TEMPLATES` 是否設定皆不影響） |
| `--template` 為名稱形式、且 `GOGENTS_TEMPLATES` 已設定 | `$GOGENTS_TEMPLATES/<name>/` |
| `--template` 為名稱形式、且 `GOGENTS_TEMPLATES` 未設定 | `<當前目錄>/<name>/` |
| 未給 `--template` | 內建 embed（`templates/base/`） |

**不做來源疊加**：選定的樹即全部內容，不與 embed 合併。使用者只想改 `.claude/CLAUDE.md` 一個檔，仍需複製整棵 `templates/base/` 後修改——release 產物與 repo 都帶著 `templates/`，複製成本近乎零，換得的是「看到什麼樹就落什麼檔」的可預測性，不會有「我沒放這個檔卻冒出來」的合成結果。

**無名稱回退**：名稱形式解析不出目錄即報錯（§3.5），不改試其他名稱、不落回 embed。`base` 不是特殊名稱——embed 樹只在完全未給 `--template` 時生效，不參與名稱解析。

**`GOGENTS_TEMPLATES` 是名稱形式的展開基底，不是搜尋路徑**：它只有單一路徑值（非 `PATH` 式的多路徑串接），且只在名稱形式下生效——設了變數但未給 `--template` 仍走 embed。

### 3.3 路徑形式與名稱形式的判別

`--template` 的值含路徑分隔符即為**路徑形式**，否則為**名稱形式**。

| 值 | 形式 | 解析結果 |
| --- | --- | --- |
| `./my-tpl`、`../tpl`、`/opt/tpl`、`a/b` | 路徑 | 該目錄 |
| `rust-svc` | 名稱 | `$GOGENTS_TEMPLATES/rust-svc/`，或未設變數時 `./rust-svc/` |

判別只看值的字面形式，不看目錄是否存在：`rust-svc` 永遠是名稱，即使當前目錄剛好沒有它；要指定當前目錄下的樹又想繞過 `GOGENTS_TEMPLATES`，寫 `./rust-svc`。

### 3.4 template 樹契約

**外部樹與 embed 樹同構**：指定的目錄本身即樹根，其下的相對路徑就是 workspace 內的相對路徑，materialize 不做任何路徑映射，樹根與 workspace 根之間**不存在額外的 `templates/` 中介層**。

```
--template ./my-tpl

./my-tpl/                       →  <workspace>/
├── .gogents/
│   └── runtime.yaml.tmpl       →  .gogents/runtime.yaml
├── .claude/
│   └── CLAUDE.md               →  .claude/CLAUDE.md
└── .mcp.json                   →  .mcp.json
```

repo 內 `templates/` 是收納目錄、`base/` 才是樹根（embed 亦 `fs.Sub` 到 `base/` 才打包），故 `$GOGENTS_TEMPLATES` 對應的是收納目錄、`$GOGENTS_TEMPLATES/<name>/` 才是樹根——與 repo 佈局一致，使用者把 `templates/base/` 整個複製到 `$GOGENTS_TEMPLATES/<name>/` 即可用。

materialize 規則（`.tmpl` render 去後綴、其他檔純複製、`0o600`、原子寫入、不刪除既有內容）與資料模型（`Spec` 的欄位集）沿用 CH-007，外部樹與 embed 樹適用同一套規則。**外部樹可引用的變數即 `Spec` 的欄位**，引用未知欄位由 `missingkey=error` 於 render 時報錯。

**只處理一般檔案與目錄**：外部樹可能含 embed 不可能產生的項目（symlink、device 等）。非一般檔案一律報 `ErrTemplateInvalid`，不靜默略過——略過會產出一個看似成功、卻缺檔的 workspace。

### 3.5 錯誤處理

| 情境 | 處理 |
| --- | --- |
| `--template` 路徑形式，目錄不存在 | `ErrTemplateNotFound`、exit ≠ 0、不寫入 |
| `--template` 名稱形式，展開後的目錄不存在 | `ErrTemplateNotFound`、exit ≠ 0、不寫入；訊息列出實際查找的絕對路徑與其來自 `GOGENTS_TEMPLATES` 或當前目錄 |
| `--template` 解析到的路徑存在但非目錄 | `ErrTemplateNotFound`、exit ≠ 0、不寫入 |
| `--template` 值為空字串（`--template=`） | 旗標驗證錯誤、exit ≠ 0；不等同省略 |
| 外部樹含非一般檔案項目 | `ErrTemplateInvalid`、exit ≠ 0；已寫入的檔案保留並列出（CH-007 C5） |

**外部來源失敗一律報錯，不落回 embed**：使用者明示 `--template` 就是要那棵樹；靜默落回會產出一個「看起來成功、內容卻不是他要的」workspace，比失敗更難察覺。

**不驗證樹的內容**：樹內缺 `runtime.yaml.tmpl`、或 render 出的 runtime.yaml 不合 schema，`create` 皆不預先攔截——materialize 是無寫死單檔邏輯的通用規則（CH-007 §3.5），且外部樹的用途本就由使用者定義。後果由 `gogents start` 的既有 schema 驗證呈現。此處刻意不保 CH-007 C6（產出可直接 `start`）：C6 的擔保對象是內建 template，外部樹的正確性歸使用者。

`create` 為短命 CLI：錯誤寫 stderr + 非零 exit，不寫 daemon 日誌。

## 4. 交付物

**文件先行**：以下 canonical 文件於實作前先更新到最新狀態。

| 落點 | 內容 |
| --- | --- |
| `docs/requirements/reqs/R13-workspace-provisioning.md` | §Template 改寫：template 為可替換來源（解析順序、不疊加、外部樹契約、失敗不落回） |
| `docs/designs/D09-provision.md` | 來源解析（`Resolve`）、`ErrTemplateNotFound`、非一般檔案處理 |
| `docs/designs/D08-runtime-cli.md` | `runtime create` 參數表新增 `--template`；錯誤對應表新增解析失敗列 |
| `docs/tests/unit-tests.md` | 新增來源解析的案例清單 |
| `AGENTS.md`、`README.md` | `GOGENTS_TEMPLATES` 環境變數說明、自訂 template 的用法 |

**程式落點**：

| 落點 | 內容 |
| --- | --- |
| `internal/provision/` | 來源解析（路徑／名稱形式判別、`GOGENTS_TEMPLATES` 展開、目錄檢查）、`ErrTemplateNotFound`、materialize 的非一般檔案檢查 |
| `internal/cli/` | `--template` 旗標解析與驗證；`Create` 的來源由 `Builtin()` 寫死改為解析結果 |

單元測試涵蓋：路徑形式與名稱形式的判別、名稱形式在 `GOGENTS_TEMPLATES` 設定／未設定下的展開落點、路徑形式無視 `GOGENTS_TEMPLATES`、未給 `--template` 走 embed、設了變數但未給 `--template` 仍走 embed、外部樹的檔案正確落地（含 `.tmpl` render）、外部樹不與 embed 合併（embed 有而外部樹無的檔案不得出現）、解析失敗的各錯誤路徑且不寫入、非一般檔案報錯。

## 5. 相容性

- **`--template` 為新增選用旗標**：省略時來源仍為 embed，CH-007 的 `runtime create` 行為與輸出完全不變。
- **`GOGENTS_TEMPLATES` 為新增環境變數**：未設定時行為不變；設定後亦僅在名稱形式的 `--template` 下生效，不影響任何既有指令。與 `GOGENTS_HOME` 各自獨立——全域家目錄是註冊表與日誌的狀態目錄，不作為 template 落點。
- **runtime.yaml schema 不變**：本變更只改「範本從哪來」，不改產出檔的 schema。
- **`templates/base/` 與 embed 不變**：內建樹的內容與打包方式沿用 CH-007。
- **am 不受影響**：am 續用自己的 `internal/provision` 與 `templates/`，本變更未觸碰 am 的任何路徑或契約。

## 6. 關聯

- 相依 CH-007（`change-runtime-create.md`）的 `internal/provision` 與 `templates/base/`；materialize 規則、資料模型、非空目錄確認、不刪除不變量皆沿用該變更，本變更不重述。
- 佈建需求：R13；provision 模組設計：D09；CLI 分派與子指令契約：D08、`internal/cli/cli.go`。
- CH-009（local source 去 am 化）與本變更無相依，可並行。
