# CH-009 — local 任務源去 am 化（inbox 預設路徑與 ticket 契約歸屬）

## 1. 背景與動機

gogents 公開化後定位為**個人／本機 agent 的專業運作環境工具**，可獨立運作、不依賴 agent-manager（am）。am 從「控制面／Tower」降格為 local 任務源的其中一種 ticket producer。

`local` 任務源目前仍以 am 為隱含前提：

- **預設 inbox 寫死 `~/.am/tickets`**：`source.path` 省略時，gogents 會去讀一個屬於第三方工具的目錄。對不裝 am 的使用者，預設值指向永不存在的路徑，前置檢查必然失敗且訊息無從自解釋；`GOGENTS_HOME` 做環境隔離時，此路徑也不隨之隔離。
- **ticket schema 記為「欄位由 am 定義」**：gogents 讀寫的契約真相源掛在外部專案上，單獨看 gogents 的文件無法得知該寫什麼欄位才能餵任務給它。

本變更把 local 任務源的預設落點與 schema 契約收回 gogents 自己，使其不依賴任何特定 producer 即可完整說明與運作。

## 2. 變更目標

1. `source.path` 省略時的預設 inbox 由 `~/.am/tickets` 改為 `<全域家目錄根>/tickets`，隨 `GOGENTS_HOME` 隔離。
2. ticket schema 升格為 **gogents 自己的本機任務源契約**（R11 為真相源）；am 與其他工具皆為符合此契約的 ticket producer。
3. `status: queued` 的表述由「am 的 dispatch 閘門」改為 producer 中立的認領規則。

## 3. 具體需求規格

### 3.1 預設 inbox 路徑

- `source.path` 省略時 → `<全域家目錄根>/tickets`；全域家目錄根依 R01 解析（`GOGENTS_HOME` → `$HOME/.gogents` → `/tmp` fallback）。
- `source.path` 顯式設定時行為不變：原樣採用，僅展開開頭的 `~/`。
- inbox **不自動建立**：前置檢查（R02 階段 2）維持「不存在或不可讀即拒啟並記 error」。inbox 由 producer 擁有並寫入，gogents 只讀；代建會讓路徑打錯時 daemon 靜默空轉，而非在啟動時就報錯。

### 3.2 ticket 契約歸屬

ticket schema 為 gogents 的本機任務源契約，欄位擁有權如下：

| 欄位群 | 擁有者 | gogents 行為 |
| --- | --- | --- |
| `id` / `worker` / `goal` / `context` / `verify` | producer | 只讀 |
| `status` | 共同 | 讀（認領閘門）／寫（終態 `done` / `failed`） |
| `result` / `updated_at` | gogents | 寫（終態結果與時間） |
| 其他任何欄位 | producer | 不讀不寫，回寫時原樣保留 |

producer 治理用的欄位（am 的 `mission` / `depends_on` / `approvals` / `risk` 等）屬「其他任何欄位」，gogents 不賦予語義。

### 3.3 認領規則的 producer 中立表述

`status == queued` 是 gogents 的認領條件：gogents 只認領 `queued`，其餘值（含空值、無法解析）一律不認領，dispatch 時機由 producer 以 `status` 掌控。文件不再把此規則寫成 am 的 DAG 前置語義。

## 4. 交付物

- `internal/source/local.go`：inbox 預設值改由 `foundation.GlobalHome()` 推導；`TaskContent.Author` 由固定字串 `am` 改為 `local`（標示來源型別，不再指名 producer）。
- `internal/foundation/config.go`：`source.path` 預設值註解同步。
- 對應單元測試：`source.path` 省略時 inbox 落在 `<GOGENTS_HOME>/tickets`；顯式 `source.path` 不受影響。
- `docs/` 落點（canonical 最新狀態）：
  - `requirement.md`：本地任務源檔案表、術語表（Ticket / Task）。
  - `R01`：全域家目錄佈局納入 `tickets/`。
  - `R02`：`local` 區塊預設值、必填欄位、前置檢查。
  - `R11`：ticket 契約歸屬與認領規則（本變更的主要落點）。
  - `D01` / `D07`：`source.path` 預設值、模組邊界與行為契約。
  - `unit-tests.md`（D01-17）、`design-plan.md`（D07 摘要）。
  - `README.md` / `AGENTS.md`：inbox 預設路徑事實。

## 5. 相容性

- **am 零影響**：am 一律以絕對路徑寫入 `runtime.yaml` 的 `source.path`（CH-006 §3.3 盤點確認），走不到本變更的 fallback。
- 顯式設定 `source.path` 的既有 runtime 行為不變。
- 未設 `source.path` 且倚賴 `~/.am/tickets` 預設值的 runtime 需改為顯式指定該路徑。
- ticket 檔格式與回寫語義不變，僅契約歸屬與文件表述改變。

## 6. 關聯

- 全域家目錄根解析：CH-006 §3.1、R01。
- 認領閘門既有語義：CH-005（`status: queued` 過濾）。
- 公開化定位收尾（README／AGENTS／R01–R12 定位重寫、LICENSE、release pipeline）→ CH-010。
