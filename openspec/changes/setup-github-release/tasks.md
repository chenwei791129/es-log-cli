## 1. 授權檔

- [x] [P] 1.1 新增 `LICENSE`,內容為標準 MIT License 全文,版權行為 `Copyright (c) 2026 Chen-Wei Sun`。完成時:repo 根目錄存在 `LICENSE`,GitHub 能辨識為 MIT。

## 2. GitHub Actions 工作流程

- [x] [P] 2.1 新增 `.github/workflows/go-test.yml`:於 push 到非 `main` 分支時觸發(`on.push.branches-ignore: [main]`),checkout 後以 `actions/setup-go` 搭配 `go-version-file: go.mod` 安裝 Go,並執行 `go test ./...`。實作 spec 需求 "Test workflow on non-main pushes"。完成時:推送 feature 分支會跑測試、推送 `main` 不會。
- [x] [P] 2.2 新增 `.github/workflows/pr-build.yml`:於 `pull_request` 目標為 `main` 時觸發,以 matrix 對 `linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64` 各跑一次 `CGO_ENABLED=0 go build ... ./cmd/es-log`,`fail-fast: false`,任一平台編譯失敗即讓 workflow 失敗。`permissions: contents: read`。實作 spec 需求 "Pull request cross-platform build validation"。完成時:PR 會驗證 4 個平台皆能編譯。
- [x] [P] 2.3 新增 `.github/workflows/release-please.yml`:`on.push.branches: [main]`,`permissions` 含 `contents: write`、`pull-requests: write`、`issues: write`。job 一使用 `googleapis/release-please-action@v4`、`release-type: go`、`token: ${{ secrets.GITHUB_TOKEN }}`,輸出 `release_created` 與 `tag_name`。job 二在 `release_created` 為真時,以與 2.2 相同的 4 平台 matrix build,build 指令注入版本:`-ldflags "-s -w -X github.com/chenwei791129/es-log-cli/internal/cmd.version=<tag_name>"` 並 `-o <平台命名> ./cmd/es-log`,最後用 `gh release upload <tag_name> <檔案> --clobber` 上傳為 release asset。實作 spec 需求 "Release-please manages releases on main" 與 "Versioned cross-platform release assets"。完成時:合併 release PR 後會產生 release 並附上 4 個平台 binary。

## 3. README 文件

- [x] 3.1 改寫 `README.md`:(a)`## Installation` 段改以「從 GitHub Releases 下載對應平台 pre-built binary」為主路徑,提供 `curl -L` 下載 + `chmod +x` + 移到 PATH 的範例,涵蓋 4 個平台的 asset 命名;(b)將現有 `make build` 從原始碼建置的說明降級,移到 `## Development` 段作為次要選項;(c)`## License` 段由 `MIT License.` 改為說明採用 MIT License 並指向 `LICENSE` 檔。完成時:讀者無需 Go 工具鏈即可依 README 安裝。

## 4. 驗證

- [x] 4.1 以 `actionlint`(或退而求其次的 YAML 解析)檢查三個 workflow 檔語法正確、無未定義的 `secrets`/`needs` 參照。完成時:三個 workflow 皆通過 lint。
- [x] 4.2 本地以 `GOOS`/`GOARCH` 對 4 個目標平台各跑一次 `CGO_ENABLED=0 go build -ldflags "-s -w -X github.com/chenwei791129/es-log-cli/internal/cmd.version=vtest" -o /dev/null ./cmd/es-log`,確認交叉編譯與版本注入路徑(`internal/cmd.version`、`./cmd/es-log` 進入點)皆正確。完成時:4 個平台皆成功編譯。

## 5. 建立 GitHub repo 與初次發佈

- [ ] 5.1 確認 `gh auth status` 已登入後,以 `gh repo create chenwei791129/es-log-cli --public --source=. --remote=origin --description "Read-only, agent-friendly Elasticsearch log query CLI"` 建立公開 repo 並把它設為 `origin` remote(filter-repo 改寫歷史時已移除舊 remote,故需重新加入)。完成時:`git remote -v` 顯示 origin 指向新建的公開 repo,且 repo 尚無 commit。
- [ ] 5.2 以 `git push -u origin main` 推送改寫後的乾淨歷史(含 LICENSE、三個 workflow、更新後 README)。完成時:GitHub repo 的 Actions 分頁出現三個 workflow,且本次對 `main` 的 push 觸發 `release-please.yml` 並開出第一個 release PR。
