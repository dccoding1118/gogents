# CH-006 — 全域家目錄可配置化與環境隔離機制（`GOGENTS_HOME`）

## 1. 背景與動機

`agent-manager`（am）為 gogents 的控制面，近期完成 dev/prod 環境切分：am 側全域狀態改由 `AM_HOME` 覆寫（dev 綁 sandbox、prod 綁營運目錄），使開發環境與正式營運環境互不污染。

gogents 側的全域家目錄（runtime 註冊表 `config.yaml` 與 `logs/`）目前寫死為 `$HOME/.gogents/`、無環境變數可覆寫。因此：

- **dev/prod 共用同一份 runtime 註冊表**：同一台機器上開發用與營運用的 daemon 都往 `~/.gogents/config.yaml` 註冊，彼此混雜。am e2e 目前只能靠「開跑前後 best-effort 清掉 workspace 落在 sandbox 底下的 test runtime」來隔離，脆弱且非對稱（am 已能以 `AM_HOME` 乾淨切開，gogents 不能）。
- 無法在同機跑「一套 dev、一套 prod」而各自持有獨立註冊表與日誌。

本變更讓 gogents 的全域家目錄可經環境變數覆寫，與 am 的 `AM_HOME` 對稱，使 dev/prod 能在同機各持獨立全域狀態。

## 2. 變更目標

1. 新增環境變數 `GOGENTS_HOME`，覆寫全域家目錄根（預設維持 `$HOME/.gogents`）。
2. 全域家目錄的解析**收斂到單一來源**，`config.yaml` 與 `logs/` 皆據此推導，消除目前兩處各自 `os.UserHomeDir()` 的重複解析。
3. 盤點 runtime 啟動與觀測路徑中對 `~/.gogents`／`~/.am` 的絕對路徑假設，確認除全域家目錄外無其他寫死點需一併配置化。
4. 盤點其餘可環境化的參數，確立判準（env 只承載部署環境識別；per-runtime 行為留 `runtime.yaml`）；結論為**除 `GOGENTS_HOME` 外不再新增其他環境變數、亦不導入 `.env` 檔載入機制**。
5. 新增 `gogents runtime remove <workspace|name>` 反註冊命令，讓 am 以正式介面清除退役／測試 runtime，取代手改 `config.yaml`。

## 3. 具體需求規格

### 3.1 `GOGENTS_HOME` 覆寫

- 全域家目錄根的解析順序：
  1. 若環境變數 `GOGENTS_HOME` 非空 → 逕用其值當全域家目錄根。
  2. 否則 → `$HOME/.gogents`（現行行為，向後相容）。
  3. `$HOME` 亦無法解析時 → 維持現行 `/tmp` fallback。
- 覆寫的是**全域家目錄根**本身（即目前的 `~/.gogents`）：
  - runtime 註冊表：`<home>/config.yaml`
  - 日誌：`<home>/logs/<hash>/{daemon,error}.jsonl`
- **per-workspace 的 `.gogents/`**（`runtime.yaml`／`state.yaml`／`tasks/`）位於 workspace 目錄之下、屬相對路徑，**不受本變更影響**、不隨 `GOGENTS_HOME` 移動。

### 3.2 解析收斂到單一來源

- 現況：全域家目錄在兩處各自獨立以 `os.UserHomeDir()` 推導——
  - `internal/foundation/workspace.go`：`globalHome = filepath.Join(home, ".gogents")`（供 `GlobalConfigYaml()`、`LogsDir()`）。
  - `internal/foundation/global_config.go`：`GlobalConfigPath()` 另行以 `os.UserHomeDir()` 組出 `home/.gogents/config.yaml`。
- 變更：兩者改讀**同一個** home 解析函式（含 `GOGENTS_HOME` 覆寫邏輯），避免覆寫只生效於其一而產生註冊表與觀測路徑指向不同目錄的裂縫。

### 3.3 絕對路徑假設盤點

- 確認除 §3.1 全域家目錄外，daemon 啟動、runtime 註冊、觀測 CLI（`runtime list/status`、`logs`）路徑中無其他寫死 `~/.gogents`／`~/.am` 的絕對路徑。
- **`runtime.yaml` 的 `source.path`（`local` 源指向 am 的 ticket inbox）由 am 產生並以絕對路徑寫入**，屬 am 側 `AM_HOME` 契約，gogents 僅讀取、不需改動；本項只做盤點確認，非行為變更。

### 3.4 其他可環境化參數盤點（結論：不新增其他環境變數）

**判準**：環境變數只承載**部署環境識別**——「這個 process 跑在哪個環境」，使同機可並存多套隔離環境（dev/prod）。**per-runtime 行為**（sync 頻率、失敗策略、模型、時區、assignee）留在 `runtime.yaml`，因其隨 workspace/runtime 而異、非隨環境而異；抽成 env 會製造雙來源並拉錯 altitude。

現況盤點（gogents 目前**全無** env 變數，僅隱式讀 `$HOME`）：

| 參數 | 現況 | 歸類 | 決策 |
| --- | --- | --- | --- |
| 全域家目錄根 | 寫死 `~/.gogents`（`workspace.go` / `global_config.go`） | 環境識別 | **`GOGENTS_HOME`**（§3.1，本變更唯一新增的 env 變數） |
| am ticket inbox 預設 | 寫死 `~/.am/tickets`（`source/local.go` `defaultTicketInbox`），為 `source.path` 省略時的 fallback | 環境識別 | **不新增 env**：主契約是 am 以絕對路徑寫入 `source.path`（§3.3），gogents 讀取不推導；環境隔離由 am 側 `AM_HOME` 保證 |
| 工具鏈執行檔 `gh` / `claude` / `git` | 以 bare name 經 `PATH` 解析（`exec.Command` / `LookPath`） | 環境識別 | **不新增 env**：`PATH` 已足以在不同環境指向不同工具，無額外旗標需求 |
| `source.sync_interval` / `task_policy.max_failures` / `agent.model` / `timezone` / `assignee` | `runtime.yaml` 欄位 | per-runtime 行為 | **維持 `runtime.yaml`，不抽 env** |
| agent probe timeout（`claude --version` 10s）、git probe | 寫死常數 | 內部細節 | 不抽；無環境差異需求 |

**結論**：本變更僅新增 `GOGENTS_HOME` 一個環境變數；**不**導入 `.env` 檔載入機制，其餘參數維持現況（`runtime.yaml` / `PATH` / am 契約）。

### 3.5 `gogents runtime remove <workspace|name>`（反註冊命令）

- 目前 gogents 僅有 `start` 的 upsert 註冊與 `runtime list/status`、`logs` 觀測，**無反註冊命令**；am e2e 只能直接手改 `~/.gogents/config.yaml` 清掉退役／測試 runtime。
- 新增 `gogents runtime remove <workspace|name>`：以 `Resolve` 相同的比對規則（workspace 路徑優先、否則 name）定位一筆註冊，自 `config.yaml` 的 `runtimes[]` 移除，原子寫入（write-tmp → fsync → rename，同 upsert 風格）。
- 找不到對應 runtime → 回 `ErrRuntimeNotRegistered`、exit ≠ 0（與 `status` / `logs` 對未註冊目標的行為一致）；不改動 `config.yaml`。
- **只動註冊表**：不觸碰該 runtime 的 `logs/`、workspace 內 `.gogents/`，亦不嘗試停止其 daemon（反註冊 ≠ 停機）。
- 與 `GOGENTS_HOME` 相輔：環境隔離（獨立註冊表）+ 乾淨反註冊（正式介面），讓 am e2e 完全不需手改檔案。

## 4. 交付物

- `internal/foundation/`：單一 home 解析函式（含 `GOGENTS_HOME` 覆寫）；`workspace.go` 與 `global_config.go` 改共用之。
- `GlobalConfigStore.Remove(workspaceOrName string) error`（§3.5）+ `gogents runtime remove` CLI 子命令。
- 對應單元測試：
  - `GOGENTS_HOME` 設定時全域路徑（config、logs）指向覆寫目錄；未設定時維持 `$HOME/.gogents`。
  - `Remove`：命中（workspace / name）後該筆自 `runtimes[]` 消失且其餘不動；未命中回 `ErrRuntimeNotRegistered`。
- `docs/` 對應需求（R01/R02/R09/R12）、設計（D01/D08）與 README/AGENTS 更新（`GOGENTS_HOME`、`runtime remove`）。

## 5. 相容性

- **預設行為不變**：未設 `GOGENTS_HOME` 時，全域家目錄仍為 `$HOME/.gogents`，既有部署零影響。
- per-workspace 的 `.gogents/` 佈局與語意不變。
- 跨系統契約（am 產生給 gogents 讀的 `runtime.yaml`）不變。
- **`runtime remove` 為新增子命令**：不影響 `start` / `list` / `status` / `logs` 既有行為。

## 6. 關聯

- am 環境切分需求：`~/workspace/agent-manager/docs/design.md` §2.1–§2.5（dev/prod 雙環境、`AM_HOME` 契約）、§6「gogents 環境可配置化」；`docs/deploy.md`。
- gogents 全域家目錄現況：`internal/foundation/workspace.go`、`internal/foundation/global_config.go`；runtime 註冊來源 CH-002 §3.5。
