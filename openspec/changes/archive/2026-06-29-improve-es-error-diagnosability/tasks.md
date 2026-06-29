## 1. esclient：暴露 reason 與 shard 失敗資訊

實作 spec 需求「Error response root-cause extraction」；對應設計決策「共用 deep-reason 解析器置於 esclient」「APIError 取出並展開 root-cause reason」「`_shards.total` 納入回應解析」。

- [x] 1.1 依設計決策「`_shards.total` 納入回應解析」，在 `SearchResponse._shards` 結構補上 `total` 欄位（與既有 `failed`/`failures` 並列），使命令層能輸出 `N of M shards failed`。
- [x] 1.2 依設計決策「共用 deep-reason 解析器置於 esclient」，在 `internal/esclient/client.go` 新增單一可重用的失敗 reason 解析函式：輸入非 2xx error body 或單一 `_shards.failures[]` 元素，輸出最深可得的 reason 字串。非 2xx body 取值優先序為 `error.root_cause[0].reason` → `error.failed_shards[0].reason.reason` → `error.reason`；`_shards.failures` 取 `failures[0].reason.reason`，type 作 fallback；皆無法解析時回傳空字串。
- [x] 1.3 依設計決策「APIError 取出並展開 root-cause reason」，為 `APIError` 新增 root-cause reason 欄位，於建構（非 2xx 回應）時以 1.2 的解析器填入，並提供帶出 `error.type` 與 reason 的描述方法供命令層使用；reason 為空時退回僅含 type 的描述。實作需求 Error response root-cause extraction。
- [x] 1.4 在 `internal/esclient/client_test.go` 新增針對自有程式碼的測試：reason 深挖優先序（root_cause 優先於 failed_shards 優先於 reason）、不可解析時回空字串、`_shards.total/failed/failures` 正確解析；`go test ./internal/esclient/...` 通過。

## 2. exit.go：新增 exitPartial 並在訊息帶出 root cause

對應 spec 需求「Errors on stderr with layered exit codes」；對應設計決策「新增 exitPartial=5 代表結果殘缺」「partial 診斷統一以非零退出的錯誤訊息呈現」。

- [x] 2.1 依設計決策「新增 exitPartial=5 代表結果殘缺」，在 `internal/cmd/exit.go` 新增常數 `exitPartial = 5`（語意：查詢有回應但結果殘缺）。
- [x] 2.2 修改 `classifyESError`：非 404/401/403 分支輸出形如 `elasticsearch error (HTTP <code>: <error.type> — <reason>)`，reason 取自 1.3 的 `APIError`；reason 為空時退回原本僅含 type 的訊息。404/401/403 與 context 取消分支行為不變。實作需求 Errors on stderr with layered exit codes。
- [x] 2.3 依設計決策「partial 診斷統一以非零退出的錯誤訊息呈現」，新增建構 partial-failure 結束錯誤的輔助（exit code `exitPartial`、訊息形如 `incomplete results: N of M shards failed (<reason>)`）。
- [x] 2.4 在 `internal/cmd/exit_test.go` 更新／新增測試：default 分支訊息含 reason、reason 為空時的 fallback、partial 結束錯誤的 exit code 為 5 與訊息格式；`go test ./internal/cmd/ -run Exit` 通過。

## 3. search.go：hit 路徑偵測 partial（含分頁）

實作 spec 需求「Partial shard failure surfacing for searches」；對應設計決策「hit 路徑偵測 shard 失敗並貫穿分頁」。

- [x] 3.1 [P] 依設計決策「hit 路徑偵測 shard 失敗並貫穿分頁」，讓 hit 路徑（`singleSearch` 與 `paginateAll`）把 `_shards` 失敗資訊上浮到命令層：單次取該回應的 `total`/`failed`/首個 reason；分頁則任一頁 `_shards.failed > 0` 即標記 partial、保留首個 reason、累計失敗 shard 數。
- [x] 3.2 [P] 在 `runSearch` 中，當結果標記為 partial 時依序：先 render 已取得的 hits 到 stdout → 再以 2.3 的輔助輸出單行 `error:` 診斷到 stderr → 回傳帶 `exitPartial` 的結束錯誤。完全成功（failed==0）行為不變。實作需求 Partial shard failure surfacing for searches。
- [x] 3.3 [P] 在 `internal/cmd/search_test.go` 新增測試：單次 search partial 時 stdout 含 hits 且 exit 5、分頁任一頁 partial 時累計失敗數與首個 reason 正確、failed==0 時 exit 0 且無診斷；`go test ./internal/cmd/ -run Search` 通過。

## 4. aggregation.go：partial 改為非零退出並共用解析器

實作 spec 需求「Partial shard failure surfacing for aggregations」；沿用設計決策「共用 deep-reason 解析器置於 esclient」「partial 診斷統一以非零退出的錯誤訊息呈現」。

- [x] 4.1 [P] 將 `internal/cmd/aggregation.go` 既有 `shardFailureReason()` 改為呼叫 1.2 的共用解析器，移除重複實作。
- [x] 4.2 [P] 修改 `runAggregation`：`_shards.failed > 0` 時改為先 render 殘缺 aggregations 到 stdout → 以 2.3 的輔助輸出 `error:` 診斷到 stderr → 回傳帶 `exitPartial` 的結束錯誤（取代既有 `Warnf` + exit 0）。實作需求 Partial shard failure surfacing for aggregations。
- [x] 4.3 [P] 在 `internal/cmd/aggregation_test.go` 更新測試：partial aggregation 時 stdout 含 aggregations 且 exit 5、stderr 訊息格式為 `incomplete results: N of M shards failed (...)`；`go test ./internal/cmd/ -run Aggregat` 通過。

## 5. 文件

- [x] 5.1 更新 `README.md`：exit code 表新增 `5 = incomplete results (partial shard failure)`，並補上含 root-cause reason 的錯誤訊息範例與 partial 範例（使用合成資料如 `Fielddata is disabled on [some_field]`、`logs-*`，不得放真實 field／index／log 內容）。

## 6. 驗證

- [x] 6.1 執行 `go test ./...` 全綠、`golangci-lint run` 無新增問題、`gofmt` 無 diff。
- [x] 6.2 請使用者以真實叢集手動驗證：故意觸發 400（如對非 aggregatable 欄位下 terms agg）確認 stderr 帶 root-cause reason 且 exit 3；觸發 partial shard 失敗確認 stdout 有殘缺結果、stderr 有 `incomplete results` 診斷且 exit 5。
