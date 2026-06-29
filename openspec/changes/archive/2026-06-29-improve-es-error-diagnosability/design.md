## Context

`es-log` 已將 Elasticsearch 查詢失敗對應到非零 exit code 且 stdout 留空，適合 script 判斷。但有兩個可診斷性缺口：

1. 非 2xx 硬失敗時，stderr 只顯示 HTTP 狀態碼與頂層 `error.type`，真正可行動的 root cause（例如 `Fielddata is disabled on [some_field]`）其實已被讀進 `APIError.Body`，卻從未被取出。
2. `200 OK` 但 `_shards.failed > 0`（部分 shard 失敗、回傳殘缺結果）時，aggregation 路徑會 warn，但 hit（文件）路徑完全不檢查 `_shards.failed`，靜默回傳殘缺結果且 exit 0。對 log 查詢工具而言，這是最危險的結果：操作者以為看到了全部符合的 log，其實有 shard 悄悄掉了。

現況相關程式：`APIError`（含 `StatusCode`/`ErrType`/`Body`，`Summary()` 只回 HTTP + type）、`classifyESError`（exit code 分層 0/2/3/4）、aggregation 路徑已有 `shardFailureReason()` 與 partial 警告（但 exit 0），hit 路徑無任何 partial 偵測。

## Goals / Non-Goals

**Goals:**

- 非 2xx 失敗的 stderr 訊息附上最深可得的 root-cause reason 與 `error.type`。
- hit 路徑（單次 search 與 `search_after` 分頁）偵測 `_shards.failed > 0`。
- partial shard 失敗在 hit 與 aggregation 兩條路徑採一致的 hybrid 契約：照印殘缺結果到 stdout、stderr 輸出含失敗 shard 數與 reason 的診斷、以新 exit code `5` 結束。
- 失敗 reason 的解析在「非 2xx body」與「`_shards.failures`」兩種來源共用同一個 parser。

**Non-Goals:**

- 不改變既有 exit code `0/2/3/4` 的語意。
- 不新增 `--allow-partial` 之類的寬鬆旗標（YAGNI）；hybrid 已同時保留資料可用性與殘缺訊號。
- 不改變成功查詢的 stdout 輸出格式。
- 不在 Spectra 文件（公開 repo）放入任何真實 field／index／log 內容，範例一律用合成資料。

## Decisions

### 共用 deep-reason 解析器置於 esclient

非 2xx body 的 `error.failed_shards[].reason` 與 200 回應的 `_shards.failures[].reason` 是相同的巢狀結構，因此將失敗 reason 解析收斂成 `esclient` 內單一可重用函式，貼近資料來源。取值優先序為 `error.root_cause[0].reason` → `error.failed_shards[0].reason.reason` → `error.reason`；對 `_shards.failures[]` 則取第一筆 `failures[0].reason.reason`，type 作為 fallback。現有 `internal/cmd/aggregation.go` 的 `shardFailureReason()` 併入此共用函式，移除重複實作。

替代方案：在 `exit.go` 各自解析。否決，因為會與 aggregation 既有的 reason 解析重複，且 reason 取值邏輯與 ES 回應結構耦合，應放在擁有該結構的 `esclient`。

### APIError 取出並展開 root-cause reason

`APIError` 新增 reason 欄位，於建構時從 body 解析填入；新增一個會帶出 reason 的描述方法（與既有 `Summary()` 並存或取代其在 `classifyESError` 的用途），讓 `classifyESError` 的非 404/401/403 分支輸出 `HTTP <code>: <error.type> — <reason>`。reason 為空時退回原本只含 type 的訊息。

替代方案：直接把整個 `Body` 印出。否決，body 過長且含雜訊，深挖單行 reason 對操作者更可讀。

### 新增 exitPartial=5 代表結果殘缺

在 `exit.go` 新增常數 `exitPartial = 5`，語意為「查詢有回應但結果不完整」。讓 script 能區分「查詢全失敗／連線失敗（3）」與「結果殘缺（5）」。

替代方案：沿用 `exitConn=3`。否決，無法讓 script 區分兩種情境，而 script 整合正是本工具的重點。

### hit 路徑偵測 shard 失敗並貫穿分頁

hit 路徑需把 `_shards` 失敗資訊從 `esclient` 回應上浮到 command 層：單次 search 取該回應的 `_shards.failed`/`total`/`failures`；`search_after` 分頁則任一頁出現 `_shards.failed > 0` 即標記 partial，捕捉首個 reason 並累計失敗 shard 數。command 層據此決定 hybrid 輸出與 exit code。

替代方案：在 `client.Search()` 內部偵測到 partial 就回傳 error。否決，那會吞掉殘缺結果，無法滿足 hybrid「照印 stdout」的要求。

### partial 診斷統一以非零退出的錯誤訊息呈現

partial shard 失敗時的輸出順序為：先 render 殘缺結果到 stdout → 再輸出單行診斷到 stderr → 回傳帶 `exitPartial` 的結束錯誤。為與「非零退出」一致，partial 的 stderr 訊息採用與其他錯誤一致的 `error:` 前綴單行框架（取代 aggregation 既有的 `warning:` 寫法），訊息形如 `incomplete results: N of M shards failed (<reason>)`。

替代方案：保留 `Warnf` 的 `warning:` 前綴並另外回傳結束錯誤。否決，會產生 stderr 雙行且語意不一致（既已非零退出，不應再稱 warning）。

### `_shards.total` 納入回應解析

`SearchResponse` 的 `_shards` 結構補上 `total`，才能輸出 `N of M shards failed` 的可讀訊息。

## Implementation Contract

**Behavior：**

- 非 2xx 硬失敗（如 HTTP 400 `search_phase_execution_exception`）：stdout 仍為空、exit code 維持 `3`，但 stderr 訊息新增 root-cause reason，形如 `error: elasticsearch error (HTTP 400: search_phase_execution_exception — Fielddata is disabled on [some_field])`。reason 不可解析時退回原本只含 type 的訊息。
- partial shard 失敗（HTTP 200 且 `_shards.failed > 0`），hit 與 aggregation 路徑一致：stdout 照常輸出已回傳的（殘缺）結果；stderr 輸出 `error: incomplete results: N of M shards failed (<reason>)`；process 以 exit code `5` 結束。
- 完全成功（`_shards.failed == 0`）：行為不變，exit `0`。

**Interface / data shape：**

- `exit.go` 新增 `exitPartial = 5`。
- `esclient` 共用函式輸入 ES 失敗結構（非 2xx body 或 `_shards.failures` 元素），輸出單行 reason 字串；`APIError` 帶出含 reason 的描述；`SearchResponse._shards` 含 `total`/`failed`/`failures`。
- hit 路徑內部回傳值新增 partial 描述（失敗 shard 數、總 shard 數、首個 reason），供 command 層決定輸出與退出碼。

**Failure modes：**

- reason 解析失敗 → 退回 type-only（非 2xx）或 `unknown reason`（`_shards.failures`），絕不 panic。
- 404／401／403 分支行為不變（exit 4／3）。
- context 取消（SIGINT 中途）維持既有 `canceled` 訊息與訊號退出碼覆寫。

**Acceptance criteria：**

- 既有測試（`internal/esclient/client_test.go`、`internal/cmd/aggregation_test.go`、`internal/cmd/search_test.go`、`internal/cmd/exit_test.go`）更新並通過。
- 新增單元測試覆蓋：非 2xx body 的 reason 深挖優先序、hit 路徑單次與分頁的 partial 偵測、partial 時 stdout 仍含結果且 exit 5、reason 不可解析時的 fallback。
- `go test ./...` 全綠；`golangci-lint` 無新增問題。

**Scope boundaries：**

- 範圍內：`internal/esclient/client.go`、`internal/cmd/exit.go`、`internal/cmd/search.go`、`internal/cmd/aggregation.go`、`README.md`、相關測試。
- 共用副作用：`classifyESError` 是共用函式，`internal/cmd/ls.go` 亦為其呼叫者；default 分支訊息附上 root-cause reason 後，`ls` 的錯誤訊息會自然一併改善，無需 `ls` 專屬程式碼變更，此屬預期且樂見。partial-shard 偵測與 exit code `5` 僅適用於做 `_shards` 查詢的 search/aggregation 路徑，不及於 `ls`/`ping`/`config`。
- 範圍外：新增旗標、變更成功輸出格式、變更 0/2/3/4 既有語意、為 `ls`/`ping`/`config` 新增 partial-shard 或 exit-5 行為。

## Risks / Trade-offs

- 新增 exit code `5` 可能影響既有 script → 緩解：`5` 是過去 partial 情境（hit 路徑原為 0、aggregation 原為 0）的新行為，不會與既有 `0/2/3/4` 衝突；於 README 明列 exit code 表。
- hybrid 在 partial 時輸出殘缺結果，下游若未檢查 exit code 可能誤用殘缺資料 → 緩解：stdout 與 stderr 同時呈現，且 exit 非零，鼓勵 script 檢查退出碼。
- aggregation 由 `warning:`／exit 0 改為 `error:`／exit 5 屬行為變更 → 緩解：於 proposal/spec 明載，並更新對應測試與 README。

## Migration Plan

單一程式內變更，無資料遷移。回滾即還原相關檔案。發佈後於 README 更新 exit code 表與錯誤訊息範例。

## Open Questions

無。
