# STATUS — gogents

## 未歸檔結論

公開化計畫（2026-07-16 定案）尚未落 `docs/` 的決策；各項於對應變更單落檔後從此處刪除。

**定位**（落點：CH-012）

- gogents 是**個人／本機 agent 的專業運作環境工具**，可獨立運作、不依賴 am。am 從「控制面／Tower」降格為 local source 的其中一種 ticket producer。

**module path**

- v2 以後需加 `/v2` path 後綴。vanity import path 評估後不採用（需自架 meta 服務，過度設計）。

**指令**

- **不新增 `init` 指令**：`config.yaml` 已是 lazy 自動建立（`Load` 缺檔回空 registry + nil，`Upsert` 的 `atomicWrite` 會 `MkdirAll`），`runtime list` 不會 fail。專建 config.yaml 的 init 屬純儀式。環境前置檢查（git / gh / claude 可用性）若要做，另名 `doctor`。

**am 收斂**

- am 最終改為呼叫 `gogents runtime create`，屆時刪除 am 的 `internal/provision` materialize 與 `templates/`。本階段**不動 am**：gogents 新增指令不會弄壞 am，且 am 一律以絕對 `source.path` 寫入 `runtime.yaml`，走不到 CH-009 改的 inbox fallback。
- am 的 `package.sh` 改抓 gogents release 產物（CH-010 已備妥發佈管道），不再從 `~/workspace/gogents` 原始碼建置。

**dc-dev-workflow 與文件標準**（2026-07-18 定案，尚無 `docs/` 落點）

- **定位/語言**：自用＋展示、不收外部 PR、樂見他人 fork 發展。README 用英文（門面／掃描可見度）；AGENTS 與內部 `docs/` 維持繁中。
- **根目錄檔案最小集**：`LICENSE`＋`README` 為必要；`CONTRIBUTING`／`CODE_OF_CONDUCT`／issue·PR template 一律省（與「不收貢獻」定位一致，寫了反矛盾；PR 格式已由個人 skill 控制）；`SECURITY`／`CHANGELOG` 為選配門面（CHANGELOG 可用 GitHub Releases 自動生成取代）。「專業門面」真正加分項是 CI＋安全掃描，非社群檔案。
- **README／AGENTS 職責切分**：README 只放使用者面（簡介・價值・how-it-works・功能・前置・安裝・啟用・操作管理・授權）；開發面（專案結構・技術棧細節・Development・測試・e2e・程式架構圖）一律歸 AGENTS。how-it-works 以**編號步驟**為主，圖為選配且**靜態圖／ASCII 優先於 mermaid**；模組關係／架構圖歸 AGENTS。完整章節格式兩張表待寫入 skill。
- **dc-dev-workflow**：把現有 `project-docs-workflow` skill 擴為五階段全生命週期（文件規劃→標準專案骨架→程式實作→e2e 驗證→正式部署＋品質/安全流程）。
- **生效路徑**：先用 gogents dogfood 實踐一輪、增減出實用的部分 → `wiki-capture` 歸納 → 於 `dc-llm-wiki` 開發目錄建置 skill、走正確路徑上版並部署到環境。**dogfood 完成前不寫 skill。**

## 未完成任務

各項逐一開隔離 session 執行。

- [ ] **CH-012 公開化定位重寫**：README／AGENTS／requirement.md／R01, R02, R04, R07, R11, R12／D01, D07, D08 拔除「gogents 是 am 的執行面」定位，改為 producer 中立敘述（am 續留為 producer 與 `--json` 消費者的實例）。程式碼註解同步：`internal/foundation/config.go`、`internal/foundation/home.go`、`internal/cli/query.go`、`internal/source/local_test.go`。不動需求／設計規格、不影響程式邏輯，故不開變更單。
- [ ] **gogents dogfood 文件標準**：README 移出開發面章節（Project Structure／Development／測試 → AGENTS）、how-it-works 改編號步驟；AGENTS 補齊缺章（維護索引已有，補 e2e／部署）；`docs/` 命名對齊複數（`designs`／`tests`／`requirements`）。與 CH-012 同觸 README／AGENTS，**兩者併一次改**避免重寫兩遍。
- [ ] **GitHub repo 安全設定啟用**（純 Settings、需 admin，非碼）：main 分支保護 + required status checks（讓 ci／codeql 從參考變合併前必過的閘門）、secret scanning + push protection、Dependabot alerts。CodeQL 已走 committed workflow（advanced setup），無需在 Security 分頁另開。
- [ ] **dc-dev-workflow skill 建置**：dogfood 修正後 → `wiki-capture` → 於 `dc-llm-wiki` 開發目錄建 skill、上版部署。取代原「把重複步驟抽成全域 skill」的暫緩項。
- [ ] **（跨 repo，另排）am／mlp LICENSE 對齊**：am 補 `LICENSE`（現 README 明寫 all rights reserved，擋 fork）；mlp 補 `LICENSE` 檔＋README License 章。推廣文件標準時一併。
