# STATUS — gogents

## 未歸檔結論

公開化計畫（2026-07-16 定案）尚未落 `docs/` 的決策；各項於對應變更單落檔後從此處刪除。

**定位**（落點：CH-010）

- gogents 是**個人／本機 agent 的專業運作環境工具**，可獨立運作、不依賴 am。am 從「控制面／Tower」降格為 local source 的其中一種 ticket producer。

**module path**

- v2 以後需加 `/v2` path 後綴。vanity import path 評估後不採用（需自架 meta 服務，過度設計）。

**指令**

- **不新增 `init` 指令**：`config.yaml` 已是 lazy 自動建立（`Load` 缺檔回空 registry + nil，`Upsert` 的 `atomicWrite` 會 `MkdirAll`），`runtime list` 不會 fail。專建 config.yaml 的 init 屬純儀式。環境前置檢查（git / gh / claude 可用性）若要做，另名 `doctor`。

**發佈**（落點：CH-010）

- gogents 自建 GitHub Actions release pipeline（tag → 多平台 binary → GitHub Releases）。am 的 `package.sh` 之後改抓 release 產物，不再從 `~/workspace/gogents` 原始碼建置。
- release 產物為 **binary + `templates/`**（排除 `*.go`，不含 `embed.go`）。壓縮檔內的 `templates/` 僅供翻閱與複製，需附 `README.md` 說明「就地改不生效，須複製後經 `--template` / `GOGENTS_TEMPLATES` 指定」，否則使用者會誤以為就地改即生效。不採「執行檔旁 templates/」的執行期搜尋（symlink／只複製 binary 就會斷，且錯誤延到執行期）。

**am 收斂**

- am 最終改為呼叫 `gogents runtime create`，屆時刪除 am 的 `internal/provision` materialize 與 `templates/`。本階段**不動 am**：gogents 新增指令不會弄壞 am，且 am 一律以絕對 `source.path` 寫入 `runtime.yaml`，走不到 CH-009 改的 inbox fallback。

**暫緩**

- 「把 CH-007～CH-010 的重複步驟標準化成全域規則或全域 skills」暫緩，待實際跑過一輪、確認哪些步驟真的重複後再抽，避免抽到臆測的流程。

## 未完成任務

各項逐一開隔離 session 執行。

- [ ] **CH-010 公開化收尾**：README／AGENTS／R01–R12 定位重寫（拔除「am 的執行面」定位）；新增 LICENSE（公開 repo 無授權條款等同保留所有權利）；GitHub Actions release pipeline。
