# STATUS — gogents

> 熱副本，只放「接手者沒寫就會遺失」的在途交接 delta。已落 docs/程式/git 的結論不重複記在此。

## 未歸檔結論

公開化計畫（2026-07-16 定案）的決策，尚未寫進 `docs/`；各項於對應變更單落檔後從此處刪除。

**定位**

- gogents 是**個人／本機 agent 的專業運作環境工具**，可獨立運作，不依賴 am。am 從「控制面／Tower」降格為 local source 的其中一種 ticket producer。
- ticket schema 升格為 gogents 自己的本機任務源契約；`status: queued` 表述為「gogents 只認領 queued，其餘狀態留給 producer 掌控 dispatch 時機」，不再寫成 am 的 dispatch gate。

**module path**

- v2 以後需加 `/v2` path 後綴。vanity import path 評估後不採用（需自架 meta 服務，過度設計）。

**指令**

- `runtime create` 建 workspace + runtime.yaml；`start` 註冊；`runtime remove` 只反註冊。create/remove 非逆操作是刻意設計——不刪使用者的 workspace 目錄。
- **不新增 `init` 指令**：`config.yaml` 已是 lazy 自動建立（`Load` 缺檔回空 registry + nil，`Upsert` 的 `atomicWrite` 會 `MkdirAll`），`runtime list` 不會 fail。專建 config.yaml 的 init 屬純儀式。環境前置檢查（git / gh / claude 可用性）若要做，另名 `doctor`。
- 簽名：`gogents runtime create <name> [--workspace <path>] --repo <url> [--branch main] --agent <t> --model <m> --source <local|github_issue> --label <l> [--tickets-dir <path>] [--instance <owner/repo>] [--template <dir|name>]`。name 為位置參數，與 `runtime status/remove` 一致。
- `--workspace` 預設 `./<name>`。不預設到 `GOGENTS_HOME` 底下——該處是註冊表與日誌的狀態目錄，不混使用者工作區。
- **不引入 specialty / tier 概念**：agent 與 model 由 `runtime create` 明確指定；`specialty@tier → model` 查表留在 am。

**template**

- **軸為專長，不含 source**。source 是 runtime.yaml 欄位，由 `--source` 在 render 時條件展開。依來源分軸會產生「專長 × 來源」笛卡兒積，而差異僅 runtime.yaml 數行。
- gogents 只出一份 `templates/base/`，`go:embed` 進 binary（`go install` 單一 binary 即可用），repo 內同時保留原始檔供 clone 後複製修改。
- 解析順序：`--template` 路徑 → `GOGENTS_TEMPLATES/<name>` → 內建 embed。
- template 內 CLAUDE.md 措辭需來源中性（「任務的目標與脈絡」），不為 local/github_issue 各出一份。
- 兩種來源的差異以 README 的 runtime.yaml 範例說明，不做成 template。

**發佈**

- gogents 自建 GitHub Actions release pipeline（tag → 多平台 binary → GitHub Releases）。am 的 `package.sh` 之後改抓 release 產物，不再從 `~/workspace/gogents` 原始碼建置。

**am 收斂**

- am 最終改為呼叫 `gogents runtime create`，屆時刪除 am 的 `internal/provision` materialize 與 `templates/`。本階段**不動 am**：gogents 新增指令不會弄壞 am，且 CH-009 改的是預設 inbox 路徑，am 一直傳絕對 `source.path`，走不到 fallback。

**暫緩**

- 第 3 點的「標準化成全域規則或全域 skills」暫緩，待 CH-007～CH-010 實際跑過一輪、確認哪些步驟真的重複後再抽，避免抽到臆測的流程。

## 未完成任務

各項逐一開隔離 session 執行。

- [ ] **新增 LICENSE**：公開 repo 無授權條款等同保留所有權利。
- [ ] **CH-007 `gogents runtime create`**：新增 `internal/provision`（materialize：copy tree + `text/template` render + 非空目錄保護 + idempotent）與 CLI。
- [ ] **CH-008 template 外部化**：`templates/base/` + `go:embed`；`--template` / `GOGENTS_TEMPLATES`。相依 CH-007。
- [ ] **CH-009 local source 去 am 化**：`internal/source/local.go` 的 `defaultTicketInbox` 由 `~/.am/tickets` 改為 `<GlobalHome>/tickets`；ticket schema 文件化為 gogents 契約。與 CH-007/008 無相依，可並行。
- [ ] **CH-010 公開化收尾**：README／AGENTS／R01–R12 定位重寫（拔除「am 的執行面」定位）；GitHub Actions release pipeline。
