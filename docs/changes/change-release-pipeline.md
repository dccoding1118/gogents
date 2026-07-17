# CH-010 — 發佈流程（授權條款與 release pipeline）

## 1. 背景與動機

gogents 公開化後，使用者取得工具的路徑必須不經原始碼建置。現況有兩個缺口：

- **無授權條款**：公開 repo 未附 LICENSE 等同保留所有權利——GitHub 服務條款僅授予檢視與 fork，不授予使用、修改、散布。使用者無權使用，貢獻者亦無從得知貢獻適用的條款。
- **無發佈管道**：唯一取得 binary 的方式是 clone 原始碼並 `mise run build`。下游（含 am 的 `package.sh`）只能從 `~/workspace/gogents` 原始碼建置，綁定單一機器的目錄佈局。

本變更為 gogents 建立授權條款與自建的 release pipeline，使 tag 即產出可直接下載使用的多平台產物。

## 2. 變更目標

1. 採 MIT 授權，repo 與 release 產物皆附條款全文。
2. 建立 GitHub Actions release pipeline：推 `v*` tag → 建置多平台產物 → 發佈至 GitHub Releases。
3. release 產物自足：內含 binary、`templates/` 與授權條款，不需 clone repo 即可完整使用（含自訂 template）。

## 3. 具體需求規格

### 3.1 授權條款

- MIT，全文置於 repo 根 `LICENSE`。
- MIT 要求條款全文隨散布副本附上，故 `LICENSE` 亦為 release 壓縮檔的必要內容，非選配。

### 3.2 平台矩陣

| GOOS | GOARCH | 納入 | 理由 |
| --- | --- | --- | --- |
| linux | amd64 / arm64 | 是 | 主要部署型態 |
| darwin | amd64 / arm64 | 是 | 個人本機執行的大宗；平台相依處全為 POSIX 語意，行為與 linux 一致 |
| windows | amd64 | 否 | `internal/cli` 的 pid 探活使用 `syscall.Kill`，於 windows 不存在，編譯即失敗 |

平台相依點僅四處：signal 攔截（`SIGTERM` / `SIGINT`）、`syscall.Kill(pid, 0)` 探活、`WaitStatus.Signaled()` 判定。相依樹無 cgo，一律 `CGO_ENABLED=0` 靜態建置，單一 linux runner 交叉編譯全平台。

### 3.3 release 產物

每個平台一個 `gogents_<version>_<os>_<arch>.tar.gz`，內容：

| 項目 | 說明 |
| --- | --- |
| `gogents` | 執行檔（`-trimpath`、`-s -w`） |
| `templates/base/` | 內建 template 的原始檔，供翻閱與複製 |
| `templates/README.md` | 說明 template 的生效路徑 |
| `LICENSE` | MIT 全文（散布必要） |
| `README.md` | 專案說明 |

另發佈 `checksums.txt`（各壓縮檔的 SHA-256）。

壓縮檔內排除 `templates/**/*.go`（即 `embed.go`）：那是 binary 的建置期產物，對產物使用者無意義。

### 3.4 template 在產物中的定位

壓縮檔內的 `templates/` **僅供翻閱與複製**：binary 使用的是編譯期 embed 的副本，就地修改壓縮檔內的 `templates/` 不會影響 `runtime create` 的輸出。必須複製一份後經 `--template` / `GOGENTS_TEMPLATES` 指定才生效。此性質須由 `templates/README.md` 明示，否則使用者會誤以為就地改即生效。

不採「執行檔旁 `templates/` 的執行期搜尋」：只複製 binary 到 `PATH`、或以 symlink 連結，搜尋即斷，且錯誤延遲到執行期才浮現。embed 是唯一來源，外部樹一律顯式指定。

### 3.5 pipeline 觸發與行為

- **觸發**：push `v*` tag。不於 PR 或 branch push 觸發。
- **測試閘門**：建置前跑 `go test ./...`，失敗即中止，不發佈。
- **發佈**：以 runner 內建的 `gh` CLI 建立 release，release notes 由 GitHub 依 commit 自動產生。權限僅需 `contents: write`。
- 除官方的 `actions/checkout` 與 `actions/setup-go` 外不使用第三方 action。

## 4. 交付物

- `LICENSE`：MIT 全文。
- `.github/workflows/release.yml`：release pipeline。
- `templates/README.md`：template 生效路徑說明（不被 embed——`//go:embed all:base` 僅涵蓋 `base/`）。
- `README.md`：新增 Install 段（下載 release 產物）；License 段由「currently unlicensed」改為 MIT。

## 5. 相容性

- 純新增：不觸碰任何執行期程式路徑、CLI 介面或設定契約。
- 既有的 clone + `mise run build` 流程不變。
- am 的 `package.sh` 得以改抓 release 產物，不再從原始碼建置；本變更不動 am。

## 6. 關聯

- template 來源解析與 `--template` / `GOGENTS_TEMPLATES`：CH-008、R13、D09。
- 產物的版本資訊（`version` 指令與建置期 `-ldflags` 注入）→ CH-011。
- 公開化定位重寫（README／AGENTS／R 系列拔除「am 的執行面」定位）→ CH-012。
